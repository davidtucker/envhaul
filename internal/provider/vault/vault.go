// Package vault reads secrets from the KV secrets engine of HashiCorp Vault
// and OpenBao, whose HTTP APIs are compatible.
package vault

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/davidtucker/envhaul/internal/config"
	"github.com/davidtucker/envhaul/internal/provider"
	"github.com/davidtucker/envhaul/internal/template"
)

// Scheme is the reference scheme, as in {{vault:app/db#password}}.
const Scheme = "vault"

const defaultMount = "secret"

// Options configure the provider.
type Options struct {
	// Source is the config's vault block. It may be nil.
	Source *config.Source
	// Online enables Fetch. Offline providers validate and describe
	// references only.
	Online bool
	// HTTPClient overrides the client built from VAULT_CACERT (tests).
	HTTPClient *http.Client
	// Getenv and HomeDir default to os.Getenv and os.UserHomeDir.
	Getenv      func(string) string
	HomeDir     func() (string, error)
	Concurrency int
	Logger      *slog.Logger
}

// Provider implements provider.Provider for Vault and OpenBao.
type Provider struct {
	opts Options

	clientOnce sync.Once
	client     *http.Client
	clientErr  error
}

// New returns a Vault provider.
func New(opts Options) *Provider {
	if opts.Concurrency <= 0 {
		opts.Concurrency = 8
	}
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	if opts.Getenv == nil {
		opts.Getenv = os.Getenv
	}
	if opts.HomeDir == nil {
		opts.HomeDir = os.UserHomeDir
	}
	return &Provider{opts: opts}
}

func (p *Provider) Scheme() string      { return Scheme }
func (p *Provider) Description() string { return "HashiCorp Vault / OpenBao" }

var versionRe = regexp.MustCompile(`:([0-9]+)$`)

// split separates a reference path into mount, secret path and version.
// Paths starting with "/" name their mount as the first segment; others use
// the configured mount.
func (p *Provider) split(path, profile string) (mount, secret, version string) {
	if m := versionRe.FindStringSubmatch(path); m != nil {
		version = m[1]
		path = strings.TrimSuffix(path, m[0])
	}
	if strings.HasPrefix(path, "/") {
		mount, secret, _ = strings.Cut(strings.TrimPrefix(path, "/"), "/")
		return mount, secret, version
	}
	mount = strings.Trim(p.opts.Source.Setting(profile, "mount"), "/")
	if mount == "" {
		mount = defaultMount
	}
	return mount, path, version
}

// Validate checks the path offline.
func (p *Provider) Validate(ref *template.Ref) error {
	mount, secret, _ := p.split(ref.Path, ref.Profile)
	switch {
	case strings.HasPrefix(ref.Path, "/") && (mount == "" || secret == ""):
		return fmt.Errorf("vault path %q needs a mount and a secret path, e.g. /kv/app/db", ref.Path)
	case secret == "" || strings.HasSuffix(secret, "/"):
		return fmt.Errorf("vault path %q must name a secret, not a directory", ref.Path)
	case strings.Contains(secret, "//"):
		return fmt.Errorf("vault path %q must not contain //", ref.Path)
	}
	return nil
}

// Request resolves the mount so that the same secret referenced with and
// without an explicit mount is fetched once.
func (p *Provider) Request(ref *template.Ref) provider.Request {
	mount, secret, version := p.split(ref.Path, ref.Profile)
	path := mount + "/" + secret
	if version != "" {
		path += ":" + version
	}
	return provider.Request{Path: path, Profile: ref.Profile, Base: mount}
}

// target is where and how one profile's requests are sent.
type target struct {
	address   string
	namespace string
	token     string
	kv1       bool
}

func (p *Provider) target(profile string) (target, error) {
	src := p.opts.Source
	t := target{
		address:   src.Setting(profile, "address"),
		namespace: src.Setting(profile, "namespace"),
		kv1:       src.Setting(profile, "kv_version") == "1",
	}
	if t.address == "" {
		t.address = p.opts.Getenv("VAULT_ADDR")
	}
	if t.address == "" {
		return t, errors.New("no Vault address: set vault.address in envhaul.yaml or VAULT_ADDR")
	}
	// @name that isn't a configured profile is the namespace.
	if t.namespace == "" && profile != "" && !src.HasProfile(profile) {
		t.namespace = profile
	}
	if t.namespace == "" {
		t.namespace = p.opts.Getenv("VAULT_NAMESPACE")
	}

	if env := src.Setting(profile, "token_env"); env != "" {
		if t.token = p.opts.Getenv(env); t.token == "" {
			return t, fmt.Errorf("no Vault token: %s (vault token_env) is empty", env)
		}
		return t, nil
	}
	if t.token = p.opts.Getenv("VAULT_TOKEN"); t.token != "" {
		return t, nil
	}
	if home, err := p.opts.HomeDir(); err == nil {
		if b, err := os.ReadFile(filepath.Join(home, ".vault-token")); err == nil {
			t.token = strings.TrimSpace(string(b))
		}
	}
	if t.token == "" {
		return t, errors.New("no Vault token: run `vault login`, set VAULT_TOKEN, or set vault.token_env in envhaul.yaml")
	}
	return t, nil
}

func (p *Provider) httpClient() (*http.Client, error) {
	p.clientOnce.Do(func() {
		if p.opts.HTTPClient != nil {
			p.client = p.opts.HTTPClient
			return
		}
		tr := http.DefaultTransport.(*http.Transport).Clone()
		if ca := p.opts.Getenv("VAULT_CACERT"); ca != "" {
			pem, err := os.ReadFile(ca)
			if err != nil {
				p.clientErr = fmt.Errorf("reading VAULT_CACERT: %w", err)
				return
			}
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(pem) {
				p.clientErr = fmt.Errorf("VAULT_CACERT %s contains no PEM certificates", ca)
				return
			}
			tr.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
		}
		p.client = &http.Client{Transport: tr, Timeout: 30 * time.Second}
	})
	return p.client, p.clientErr
}

// Fetch reads each secret's data. A secret is returned as a JSON object of
// its fields, which #key selects from. Vault has no batch read, so requests
// run concurrently.
func (p *Provider) Fetch(ctx context.Context, reqs []provider.Request) (map[provider.Request]provider.Result, error) {
	if !p.opts.Online {
		return nil, errors.New("vault provider is not configured for remote access")
	}
	client, err := p.httpClient()
	if err != nil {
		return nil, err
	}
	targets := map[string]target{}
	for _, r := range reqs {
		if _, ok := targets[r.Profile]; ok {
			continue
		}
		t, err := p.target(r.Profile)
		if err != nil {
			return nil, err
		}
		targets[r.Profile] = t
	}

	var mu sync.Mutex
	out := make(map[provider.Request]provider.Result, len(reqs))
	eg, ctx := errgroup.WithContext(ctx)
	eg.SetLimit(p.opts.Concurrency)
	for _, r := range reqs {
		eg.Go(func() error {
			res, err := p.read(ctx, client, targets[r.Profile], r)
			if err != nil {
				return err
			}
			mu.Lock()
			out[r] = res
			mu.Unlock()
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return nil, err
	}
	return out, nil
}

func (p *Provider) read(ctx context.Context, client *http.Client, t target, r provider.Request) (provider.Result, error) {
	secret := strings.TrimPrefix(r.Path, r.Base+"/")
	version := ""
	if m := versionRe.FindStringSubmatch(secret); m != nil {
		version = m[1]
		secret = strings.TrimSuffix(secret, m[0])
	}

	var u string
	if t.kv1 {
		if version != "" {
			return provider.Result{}, fmt.Errorf("vault: %s: versions need the KV v2 secrets engine (vault.kv_version is 1)", r.Path)
		}
		u = fmt.Sprintf("%s/v1/%s/%s", strings.TrimRight(t.address, "/"), escapePath(r.Base), escapePath(secret))
	} else {
		u = fmt.Sprintf("%s/v1/%s/data/%s", strings.TrimRight(t.address, "/"), escapePath(r.Base), escapePath(secret))
		if version != "" {
			u += "?version=" + version
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return provider.Result{}, err
	}
	req.Header.Set("X-Vault-Token", t.token)
	req.Header.Set("X-Vault-Request", "true")
	if t.namespace != "" {
		req.Header.Set("X-Vault-Namespace", t.namespace)
	}
	p.opts.Logger.Debug("vault read", "address", t.address, "namespace", t.namespace, "mount", r.Base, "path", secret, "version", version)

	resp, err := client.Do(req)
	if err != nil {
		return provider.Result{}, fmt.Errorf("vault: can't reach %s: %w", t.address, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return provider.Result{}, fmt.Errorf("vault: reading %s: %w", r.Path, err)
	}

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return provider.Result{}, nil
	case resp.StatusCode == http.StatusForbidden:
		return provider.Result{}, fmt.Errorf("vault: permission denied reading %s at %s: the token's policies don't allow it, or the token has expired (run `vault login`)", r.Path, t.address)
	case resp.StatusCode != http.StatusOK:
		return provider.Result{}, fmt.Errorf("vault: reading %s at %s: %s%s", r.Path, t.address, resp.Status, apiErrors(body))
	}

	var data map[string]json.RawMessage
	if t.kv1 {
		var v struct {
			Data map[string]json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(body, &v); err != nil {
			return provider.Result{}, fmt.Errorf("vault: %s: unexpected response: %w", r.Path, err)
		}
		data = v.Data
	} else {
		var v struct {
			Data struct {
				Data map[string]json.RawMessage `json:"data"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &v); err != nil {
			return provider.Result{}, fmt.Errorf("vault: %s: unexpected response: %w", r.Path, err)
		}
		data = v.Data.Data
	}
	if data == nil {
		// A deleted or destroyed version has no data.
		return provider.Result{}, nil
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return provider.Result{}, err
	}
	return provider.Result{Value: string(encoded), Found: true}, nil
}

func escapePath(p string) string {
	parts := strings.Split(p, "/")
	for i, s := range parts {
		parts[i] = url.PathEscape(s)
	}
	return strings.Join(parts, "/")
}

// apiErrors formats Vault's {"errors": [...]} body, if present.
func apiErrors(body []byte) string {
	var v struct {
		Errors []string `json:"errors"`
	}
	if json.Unmarshal(body, &v) != nil || len(v.Errors) == 0 {
		return ""
	}
	return ": " + strings.Join(v.Errors, "; ")
}
