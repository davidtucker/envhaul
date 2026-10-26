// Package gcpsecrets reads secrets from Google Cloud Secret Manager through
// its REST API, authenticating with Application Default Credentials (as
// `gcloud auth application-default login` sets them up).
package gcpsecrets

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"golang.org/x/sync/errgroup"

	"github.com/davidtucker/envhaul/internal/config"
	"github.com/davidtucker/envhaul/internal/provider"
	"github.com/davidtucker/envhaul/internal/template"
)

// Scheme is the reference scheme, as in {{gcp:db-password}}.
const Scheme = "gcp"

const cloudPlatformScope = "https://www.googleapis.com/auth/cloud-platform"

// Credentials finds a token source and the credentials' default project.
type Credentials func(ctx context.Context) (oauth2.TokenSource, string, error)

// Options configure the provider.
type Options struct {
	// Source is the config's gcp block. It may be nil.
	Source *config.Source
	// Online enables Fetch.
	Online bool
	// Credentials defaults to Application Default Credentials.
	Credentials Credentials
	// Endpoint and IAMEndpoint override the API base URLs (tests).
	Endpoint    string
	IAMEndpoint string
	HTTPClient  *http.Client
	Getenv      func(string) string
	Concurrency int
	Logger      *slog.Logger
}

// Provider implements provider.Provider for Secret Manager.
type Provider struct{ opts Options }

// New returns a Secret Manager provider.
func New(opts Options) *Provider {
	if opts.Concurrency <= 0 {
		opts.Concurrency = 8
	}
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	if opts.Credentials == nil {
		opts.Credentials = applicationDefault
	}
	if opts.Endpoint == "" {
		opts.Endpoint = "https://secretmanager.googleapis.com"
	}
	if opts.IAMEndpoint == "" {
		opts.IAMEndpoint = "https://iamcredentials.googleapis.com"
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	}
	if opts.Getenv == nil {
		opts.Getenv = os.Getenv
	}
	return &Provider{opts: opts}
}

func applicationDefault(ctx context.Context) (oauth2.TokenSource, string, error) {
	creds, err := google.FindDefaultCredentials(ctx, cloudPlatformScope)
	if err != nil {
		return nil, "", err
	}
	return creds.TokenSource, creds.ProjectID, nil
}

func (p *Provider) Scheme() string      { return Scheme }
func (p *Provider) Description() string { return "Google Cloud Secret Manager" }

var (
	nameRe     = regexp.MustCompile(`^[A-Za-z0-9_-]{1,255}$`)
	versionRe  = regexp.MustCompile(`^(latest|[0-9]+|[A-Za-z0-9_-]{1,63})$`)
	resourceRe = regexp.MustCompile(`^projects/([^/]+)/secrets/([A-Za-z0-9_-]{1,255})(?:/versions/([A-Za-z0-9_-]{1,63}))?$`)
)

// parse splits a reference path into project (may be empty), secret and
// version.
func parse(path string) (project, secret, version string, err error) {
	if m := resourceRe.FindStringSubmatch(path); m != nil {
		project, secret, version = m[1], m[2], m[3]
	} else if strings.HasPrefix(path, "projects/") {
		return "", "", "", fmt.Errorf("Secret Manager name %q must look like projects/PROJECT/secrets/NAME[/versions/VERSION]", path)
	} else {
		secret, version, _ = strings.Cut(path, ":")
		if !nameRe.MatchString(secret) {
			return "", "", "", fmt.Errorf("Secret Manager secret name %q may only contain letters, digits, - and _ (up to 255)", secret)
		}
	}
	if version == "" {
		version = "latest"
	}
	if !versionRe.MatchString(version) {
		return "", "", "", fmt.Errorf("Secret Manager version %q must be latest, a number or an alias", version)
	}
	return project, secret, version, nil
}

// Validate checks names and versions offline.
func (p *Provider) Validate(ref *template.Ref) error {
	_, _, _, err := parse(ref.Path)
	return err
}

// project resolves @profile: a configured profile's project, or the name
// itself as a project ID.
func (p *Provider) project(profile string) string {
	src := p.opts.Source
	if profile != "" && !src.HasProfile(profile) {
		return profile
	}
	return src.Setting(profile, "project")
}

// Request builds the full resource name. When no project is known offline
// it is left as "secrets/NAME/versions/V" and resolved at fetch time from
// the environment or the credentials.
func (p *Provider) Request(ref *template.Ref) provider.Request {
	project, secret, version, err := parse(ref.Path)
	if err != nil {
		return provider.Request{Path: ref.Path, Profile: ref.Profile}
	}
	if project == "" {
		project = p.project(ref.Profile)
	}
	path := fmt.Sprintf("secrets/%s/versions/%s", secret, version)
	if project != "" {
		path = "projects/" + project + "/" + path
	}
	return provider.Request{Path: path, Profile: ref.Profile}
}

// Fetch accesses each secret version.
func (p *Provider) Fetch(ctx context.Context, reqs []provider.Request) (map[provider.Request]provider.Result, error) {
	if !p.opts.Online {
		return nil, errors.New("gcp provider is not configured for remote access")
	}
	tokens := map[string]string{}
	projects := map[string]string{}
	for _, r := range reqs {
		if _, ok := tokens[r.Profile]; ok {
			continue
		}
		token, project, err := p.token(ctx, r.Profile)
		if err != nil {
			return nil, err
		}
		tokens[r.Profile], projects[r.Profile] = token, project
	}

	var mu sync.Mutex
	out := make(map[provider.Request]provider.Result, len(reqs))
	eg, ctx := errgroup.WithContext(ctx)
	eg.SetLimit(p.opts.Concurrency)
	for _, r := range reqs {
		eg.Go(func() error {
			name := r.Path
			if strings.HasPrefix(name, "secrets/") {
				project := projects[r.Profile]
				if project == "" {
					return fmt.Errorf("gcp: no Google Cloud project for %s: set gcp.project in envhaul.yaml, use @project, or set GOOGLE_CLOUD_PROJECT", name)
				}
				name = "projects/" + project + "/" + name
			}
			res, err := p.access(ctx, tokens[r.Profile], name)
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

// token returns an access token for the profile and the project to use for
// short secret names.
func (p *Provider) token(ctx context.Context, profile string) (string, string, error) {
	ts, credProject, err := p.opts.Credentials(ctx)
	if err != nil {
		return "", "", fmt.Errorf("gcp: no Google Cloud credentials: run `gcloud auth application-default login` or set GOOGLE_APPLICATION_CREDENTIALS (%w)", err)
	}
	tok, err := ts.Token()
	if err != nil {
		return "", "", fmt.Errorf("gcp: Google Cloud credentials can't be used; run `gcloud auth application-default login` again (%w)", err)
	}
	project := p.project(profile)
	for _, v := range []string{p.opts.Getenv("GOOGLE_CLOUD_PROJECT"), p.opts.Getenv("CLOUDSDK_CORE_PROJECT"), credProject} {
		if project == "" {
			project = v
		}
	}
	access := tok.AccessToken
	if sa := p.opts.Source.Setting(profile, "impersonate_service_account"); sa != "" {
		if access, err = p.impersonate(ctx, access, sa); err != nil {
			return "", "", err
		}
	}
	return access, project, nil
}

func (p *Provider) impersonate(ctx context.Context, token, account string) (string, error) {
	body, _ := json.Marshal(map[string]any{"scope": []string{cloudPlatformScope}, "lifetime": "900s"})
	u := fmt.Sprintf("%s/v1/projects/-/serviceAccounts/%s:generateAccessToken", p.opts.IAMEndpoint, account)
	var out struct {
		AccessToken string `json:"accessToken"`
	}
	status, err := p.call(ctx, http.MethodPost, u, token, body, &out)
	switch {
	case err != nil:
		return "", fmt.Errorf("gcp: impersonating %s: %w", account, err)
	case status == http.StatusForbidden:
		return "", fmt.Errorf("gcp: can't impersonate %s: your account needs roles/iam.serviceAccountTokenCreator on it", account)
	case status != http.StatusOK || out.AccessToken == "":
		return "", fmt.Errorf("gcp: impersonating %s: HTTP %d", account, status)
	}
	return out.AccessToken, nil
}

func (p *Provider) access(ctx context.Context, token, name string) (provider.Result, error) {
	p.opts.Logger.Debug("gcp access secret", "name", name)
	var out struct {
		Payload struct {
			Data       string `json:"data"`
			DataCrc32c string `json:"dataCrc32c"`
		} `json:"payload"`
		Error struct {
			Message string `json:"message"`
			Status  string `json:"status"`
		} `json:"error"`
	}
	status, err := p.call(ctx, http.MethodGet, p.opts.Endpoint+"/v1/"+name+":access", token, nil, &out)
	switch {
	case err != nil:
		return provider.Result{}, fmt.Errorf("gcp: accessing %s: %w", name, err)
	case status == http.StatusNotFound:
		return provider.Result{}, nil
	case status == http.StatusUnauthorized:
		return provider.Result{}, fmt.Errorf("gcp: credentials rejected accessing %s; run `gcloud auth application-default login` again", name)
	case status == http.StatusForbidden:
		return provider.Result{}, fmt.Errorf("gcp: permission denied accessing %s: the account needs roles/secretmanager.secretAccessor on the secret or project", name)
	case status != http.StatusOK:
		return provider.Result{}, fmt.Errorf("gcp: accessing %s: HTTP %d %s: %s", name, status, out.Error.Status, out.Error.Message)
	}

	data, err := base64.StdEncoding.DecodeString(out.Payload.Data)
	if err != nil {
		return provider.Result{}, fmt.Errorf("gcp: %s: payload is not valid base64", name)
	}
	if out.Payload.DataCrc32c != "" {
		want, err := strconv.ParseUint(out.Payload.DataCrc32c, 10, 32)
		if err != nil || crc32.Checksum(data, crc32.MakeTable(crc32.Castagnoli)) != uint32(want) {
			return provider.Result{}, fmt.Errorf("gcp: %s: payload checksum mismatch (data corrupted in transit)", name)
		}
	}
	return provider.Result{Value: string(data), Found: true}, nil
}

func (p *Provider) call(ctx context.Context, method, url, token string, body []byte, out any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := p.opts.HTTPClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return 0, err
	}
	if len(data) > 0 && json.Unmarshal(data, out) != nil && resp.StatusCode == http.StatusOK {
		return 0, errors.New("unexpected response")
	}
	return resp.StatusCode, nil
}
