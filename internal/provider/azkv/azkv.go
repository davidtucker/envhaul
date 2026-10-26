// Package azkv reads secrets from Azure Key Vault, authenticating with
// Azure's default credential chain (environment, workload and managed
// identity, then an `az login` or `azd auth login` session).
package azkv

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/security/keyvault/azsecrets"
	"golang.org/x/sync/errgroup"

	"github.com/davidtucker/envhaul/internal/config"
	"github.com/davidtucker/envhaul/internal/provider"
	"github.com/davidtucker/envhaul/internal/template"
)

// Scheme is the reference scheme, as in {{azkv:my-vault/db-password}}.
const Scheme = "azkv"

const defaultDNSSuffix = "vault.azure.net"

// API is the subset of the Key Vault secrets client used here.
type API interface {
	GetSecret(ctx context.Context, name, version string, options *azsecrets.GetSecretOptions) (azsecrets.GetSecretResponse, error)
}

// ClientFunc returns a client for a vault URL, authenticating against tenant
// (empty for the default tenant).
type ClientFunc func(ctx context.Context, vaultURL, tenant string) (API, error)

// Options configure the provider.
type Options struct {
	// Source is the config's azure block. It may be nil.
	Source *config.Source
	// Online enables Fetch.
	Online bool
	// Clients defaults to azsecrets clients using DefaultAzureCredential.
	Clients     ClientFunc
	Concurrency int
	Logger      *slog.Logger
}

// Provider implements provider.Provider for Azure Key Vault.
type Provider struct {
	opts Options

	mu      sync.Mutex
	clients map[[2]string]API
}

// New returns an Azure Key Vault provider.
func New(opts Options) *Provider {
	if opts.Concurrency <= 0 {
		opts.Concurrency = 8
	}
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	p := &Provider{opts: opts, clients: map[[2]string]API{}}
	if p.opts.Clients == nil {
		p.opts.Clients = p.defaultClients()
	}
	return p
}

// defaultClients shares one credential per tenant across vaults.
func (p *Provider) defaultClients() ClientFunc {
	var mu sync.Mutex
	creds := map[string]azcore.TokenCredential{}
	return func(_ context.Context, vaultURL, tenant string) (API, error) {
		mu.Lock()
		defer mu.Unlock()
		cred, ok := creds[tenant]
		if !ok {
			c, err := azidentity.NewDefaultAzureCredential(&azidentity.DefaultAzureCredentialOptions{TenantID: tenant})
			if err != nil {
				return nil, fmt.Errorf("azkv: no Azure credentials: run `az login`%s (%w)", tenantHint(tenant), err)
			}
			cred, creds[tenant] = c, c
		}
		return azsecrets.NewClient(vaultURL, cred, nil)
	}
}

func tenantHint(tenant string) string {
	if tenant == "" {
		return ""
	}
	return " --tenant " + tenant
}

func (p *Provider) Scheme() string      { return Scheme }
func (p *Provider) Description() string { return "Azure Key Vault" }

var (
	vaultNameRe  = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]{1,22}[A-Za-z0-9]$`)
	secretNameRe = regexp.MustCompile(`^[A-Za-z0-9-]{1,127}$`)
	versionRe    = regexp.MustCompile(`^[A-Za-z0-9]{1,64}$`)
)

// parse splits "vault/secret[:version]" or "secret[:version]" (using the
// configured default vault).
func (p *Provider) parse(path, profile string) (vault, secret, version string, err error) {
	rest := path
	if v, s, ok := strings.Cut(path, "/"); ok {
		vault, rest = v, s
	} else {
		vault = p.setting(profile, "vault")
	}
	secret, version, _ = strings.Cut(rest, ":")
	switch {
	case vault == "":
		return "", "", "", fmt.Errorf("Key Vault reference %q needs a vault: write vault-name/secret-name or set azure.vault", path)
	case !vaultNameRe.MatchString(vault):
		return "", "", "", fmt.Errorf("Key Vault name %q must be 3-24 letters, digits and hyphens, starting with a letter", vault)
	case !secretNameRe.MatchString(secret):
		return "", "", "", fmt.Errorf("Key Vault secret name %q may only contain letters, digits and hyphens (up to 127)", secret)
	case version != "" && !versionRe.MatchString(version):
		return "", "", "", fmt.Errorf("Key Vault secret version %q must be letters and digits", version)
	}
	return vault, secret, version, nil
}

// setting reads a profile's setting; @name that isn't a profile only sets
// the tenant, so other settings come from the defaults.
func (p *Provider) setting(profile, key string) string {
	if !p.opts.Source.HasProfile(profile) {
		profile = ""
	}
	return p.opts.Source.Setting(profile, key)
}

// tenant resolves @profile: a configured profile's tenant, or the name
// itself as a tenant ID.
func (p *Provider) tenant(profile string) string {
	if profile != "" && !p.opts.Source.HasProfile(profile) {
		return profile
	}
	return p.opts.Source.Setting(profile, "tenant")
}

// Validate checks vault and secret names offline; Key Vault's naming rules
// are strict, so typos are caught before any network call.
func (p *Provider) Validate(ref *template.Ref) error {
	_, _, _, err := p.parse(ref.Path, ref.Profile)
	return err
}

// Request resolves the default vault so both spellings of a secret dedupe.
func (p *Provider) Request(ref *template.Ref) provider.Request {
	vault, secret, version, err := p.parse(ref.Path, ref.Profile)
	if err != nil {
		return provider.Request{Path: ref.Path, Profile: ref.Profile}
	}
	path := strings.ToLower(vault) + "/" + secret
	if version != "" {
		path += ":" + version
	}
	return provider.Request{Path: path, Profile: ref.Profile}
}

// Fetch reads each secret.
func (p *Provider) Fetch(ctx context.Context, reqs []provider.Request) (map[provider.Request]provider.Result, error) {
	if !p.opts.Online {
		return nil, errors.New("azkv provider is not configured for remote access")
	}
	var mu sync.Mutex
	out := make(map[provider.Request]provider.Result, len(reqs))
	eg, ctx := errgroup.WithContext(ctx)
	eg.SetLimit(p.opts.Concurrency)
	for _, r := range reqs {
		vault, secret, version, err := p.parse(r.Path, r.Profile)
		if err != nil {
			return nil, err
		}
		suffix := p.setting(r.Profile, "vault_dns_suffix")
		if suffix == "" {
			suffix = defaultDNSSuffix
		}
		vaultURL := "https://" + vault + "." + suffix
		tenant := p.tenant(r.Profile)
		client, err := p.client(ctx, vaultURL, tenant)
		if err != nil {
			_ = eg.Wait()
			return nil, err
		}
		eg.Go(func() error {
			p.opts.Logger.Debug("azkv get secret", "vault", vaultURL, "name", secret, "version", version)
			resp, err := client.GetSecret(ctx, secret, version, nil)
			res, err := result(resp, err, vaultURL, r.Path, tenant)
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

func (p *Provider) client(ctx context.Context, vaultURL, tenant string) (API, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	k := [2]string{vaultURL, tenant}
	if c, ok := p.clients[k]; ok {
		return c, nil
	}
	c, err := p.opts.Clients(ctx, vaultURL, tenant)
	if err != nil {
		return nil, err
	}
	p.clients[k] = c
	return c, nil
}

func result(resp azsecrets.GetSecretResponse, err error, vaultURL, path, tenant string) (provider.Result, error) {
	if err == nil {
		if resp.Value == nil {
			return provider.Result{}, nil
		}
		return provider.Result{Value: *resp.Value, Found: true}, nil
	}
	var respErr *azcore.ResponseError
	var dnsErr *net.DNSError
	var authErr *azidentity.AuthenticationFailedError
	var authRequired *azidentity.AuthenticationRequiredError
	switch {
	case errors.As(err, &respErr) && respErr.StatusCode == http.StatusNotFound:
		return provider.Result{}, nil
	case errors.As(err, &respErr) && respErr.StatusCode == http.StatusForbidden:
		return provider.Result{}, fmt.Errorf("azkv: permission denied reading %s from %s: the identity needs the Key Vault Secrets User role (or a get-secret access policy), or the secret is disabled", path, vaultURL)
	case errors.As(err, &respErr) && respErr.StatusCode == http.StatusUnauthorized:
		return provider.Result{}, fmt.Errorf("azkv: credentials rejected by %s; run `az login%s` again", vaultURL, tenantHint(tenant))
	// When every credential in the chain is unavailable, azidentity reports
	// an unexported error whose message names DefaultAzureCredential.
	case errors.As(err, &authErr) || errors.As(err, &authRequired) || strings.Contains(err.Error(), "DefaultAzureCredential"):
		return provider.Result{}, fmt.Errorf("azkv: couldn't sign in to Azure for %s: run `az login%s` (%w)", vaultURL, tenantHint(tenant), err)
	case errors.As(err, &dnsErr):
		return provider.Result{}, fmt.Errorf("azkv: can't reach %s: check the vault name", vaultURL)
	}
	return provider.Result{}, fmt.Errorf("azkv: reading %s from %s: %w", path, vaultURL, err)
}
