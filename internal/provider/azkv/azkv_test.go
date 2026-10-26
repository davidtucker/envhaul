package azkv

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/security/keyvault/azsecrets"

	"github.com/davidtucker/envhaul/internal/config"
	"github.com/davidtucker/envhaul/internal/provider"
	"github.com/davidtucker/envhaul/internal/template"
)

// fakeKV serves secrets keyed by "vaultURL|tenant|name|version".
type fakeKV struct {
	mu      sync.Mutex
	secrets map[string]string
	errs    map[string]error
	clients []string
}

func (f *fakeKV) clientsFunc(_ context.Context, vaultURL, tenant string) (API, error) {
	f.mu.Lock()
	f.clients = append(f.clients, vaultURL+"|"+tenant)
	f.mu.Unlock()
	return kvClient{f, vaultURL + "|" + tenant}, nil
}

type kvClient struct {
	f      *fakeKV
	prefix string
}

func (c kvClient) GetSecret(_ context.Context, name, version string, _ *azsecrets.GetSecretOptions) (azsecrets.GetSecretResponse, error) {
	key := c.prefix + "|" + name + "|" + version
	if err, ok := c.f.errs[name]; ok {
		return azsecrets.GetSecretResponse{}, err
	}
	v, ok := c.f.secrets[key]
	if !ok {
		return azsecrets.GetSecretResponse{}, &azcore.ResponseError{StatusCode: 404, ErrorCode: "SecretNotFound"}
	}
	return azsecrets.GetSecretResponse{Secret: azsecrets.Secret{Value: &v}}, nil
}

func ref(t *testing.T, s string) *template.Ref {
	t.Helper()
	tmpl, err := template.Parse("{{" + s + "}}")
	if err != nil {
		t.Fatal(err)
	}
	return tmpl.Refs()[0]
}

func TestValidateAndRequest(t *testing.T) {
	src := &config.Source{
		Settings: map[string]string{"vault": "dev-kv"},
		Profiles: map[string]map[string]string{"prod": {"vault": "prod-kv", "tenant": "t-prod"}},
	}
	p := New(Options{Source: src})
	tests := []struct {
		in   string
		want provider.Request
	}{
		{"azkv:db-password", provider.Request{Path: "dev-kv/db-password"}},
		{"azkv:Other-KV/db-password", provider.Request{Path: "other-kv/db-password"}},
		{"azkv:db-password@prod", provider.Request{Path: "prod-kv/db-password", Profile: "prod"}},
		{"azkv:db-password:0123abcd@some-tenant-id", provider.Request{Path: "dev-kv/db-password:0123abcd", Profile: "some-tenant-id"}},
	}
	for _, tt := range tests {
		r := ref(t, tt.in)
		if err := p.Validate(r); err != nil {
			t.Errorf("Validate(%s): %v", tt.in, err)
		}
		if got := p.Request(r); got != tt.want {
			t.Errorf("Request(%s) = %+v, want %+v", tt.in, got, tt.want)
		}
	}
	bad := map[string]string{
		"azkv:my-kv/db_password":   "secret name",
		"azkv:k/db":                "Key Vault name",
		"azkv:1kv/db":              "Key Vault name",
		"azkv:my-kv/db:not-a-ver!": "version",
	}
	for in, want := range bad {
		if err := p.Validate(ref(t, in)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Validate(%s) = %v, want %q", in, err, want)
		}
	}
	if err := New(Options{}).Validate(ref(t, "azkv:db-password")); err == nil || !strings.Contains(err.Error(), "needs a vault") {
		t.Errorf("no default vault: %v", err)
	}
	if got := New(Options{}).Request(ref(t, "azkv:db-password")); got.Path != "db-password" {
		t.Errorf("unresolvable reference should keep its path, got %+v", got)
	}
	if p.Scheme() != "azkv" || p.Description() != "Azure Key Vault" {
		t.Error("identity")
	}
}

func TestFetch(t *testing.T) {
	fake := &fakeKV{secrets: map[string]string{
		"https://dev-kv.vault.azure.net||db-password|":         "dev-pass",
		"https://dev-kv.vault.azure.net||db-password|0123abcd": "old-pass",
		"https://prod-kv.vault.azure.net|t-prod|db-password|":  "prod-pass",
		"https://dev-kv.vault.azure.net|t-other|db-password|":  "other-tenant",
		"https://gov-kv.vault.usgovcloudapi.net||db-password|": "gov-pass",
	}}
	src := &config.Source{
		Settings: map[string]string{"vault": "dev-kv"},
		Profiles: map[string]map[string]string{
			"prod": {"vault": "prod-kv", "tenant": "t-prod"},
			"gov":  {"vault": "gov-kv", "vault_dns_suffix": "vault.usgovcloudapi.net"},
		},
	}
	p := New(Options{Source: src, Online: true, Clients: fake.clientsFunc})
	refs := []string{"azkv:db-password", "azkv:dev-kv/db-password:0123abcd", "azkv:db-password@prod", "azkv:db-password@t-other", "azkv:db-password@gov", "azkv:missing", "azkv:db-password:0123abcd"}
	var reqs []provider.Request
	for _, s := range refs {
		reqs = append(reqs, p.Request(ref(t, s)))
	}
	got, err := p.Fetch(context.Background(), reqs)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"dev-pass", "old-pass", "prod-pass", "other-tenant", "gov-pass"}
	for i, w := range want {
		if r := got[reqs[i]]; !r.Found || r.Value != w {
			t.Errorf("%s = %+v, want %q", refs[i], r, w)
		}
	}
	if got[reqs[5]].Found {
		t.Error("missing secret should not be found")
	}
	// One client per vault and tenant.
	seen := map[string]int{}
	for _, c := range fake.clients {
		seen[c]++
	}
	for c, n := range seen {
		if n != 1 {
			t.Errorf("client %s created %d times", c, n)
		}
	}
}

func TestFetchErrors(t *testing.T) {
	src := &config.Source{Settings: map[string]string{"vault": "dev-kv"}}
	tests := []struct {
		err  error
		want string
	}{
		{&azcore.ResponseError{StatusCode: 403, ErrorCode: "Forbidden"}, "Key Vault Secrets User"},
		{&azcore.ResponseError{StatusCode: 401}, "run `az login` again"},
		{&net.DNSError{Err: "no such host", Name: "dev-kv.vault.azure.net", IsNotFound: true}, "can't reach https://dev-kv.vault.azure.net: check the vault name"},
		{&azcore.ResponseError{StatusCode: 500, ErrorCode: "InternalError"}, "reading dev-kv/boom"},
		{errors.New("DefaultAzureCredential: failed to acquire a token.\nAzureCLICredential: Please run 'az login' to set up an account"), "couldn't sign in to Azure"},
	}
	for _, tt := range tests {
		fake := &fakeKV{errs: map[string]error{"boom": tt.err}}
		p := New(Options{Source: src, Online: true, Clients: fake.clientsFunc})
		_, err := p.Fetch(context.Background(), []provider.Request{p.Request(ref(t, "azkv:boom"))})
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%T: err = %v, want %q", tt.err, err, tt.want)
		}
	}

	p := New(Options{Source: src, Online: true, Clients: func(context.Context, string, string) (API, error) {
		return nil, errors.New("azkv: no Azure credentials: run `az login`")
	}})
	if _, err := p.Fetch(context.Background(), []provider.Request{p.Request(ref(t, "azkv:x"))}); err == nil || !strings.Contains(err.Error(), "az login") {
		t.Errorf("credential error: %v", err)
	}
	if _, err := New(Options{}).Fetch(context.Background(), nil); err == nil {
		t.Error("offline provider should refuse to fetch")
	}
	if _, err := New(Options{Online: true}).Fetch(context.Background(), []provider.Request{{Path: "db"}}); err == nil || !strings.Contains(err.Error(), "needs a vault") {
		t.Errorf("unresolvable path: %v", err)
	}
}

func TestDefaultClientsBuildRealClient(t *testing.T) {
	// Building a client never contacts Azure, so this runs offline: it checks
	// the credential and client wiring, and that clients are cached.
	t.Setenv("AZURE_CLIENT_ID", "")
	p := New(Options{Online: true})
	c1, err := p.client(context.Background(), "https://dev-kv.vault.azure.net", "")
	if err != nil {
		t.Fatal(err)
	}
	c2, _ := p.client(context.Background(), "https://dev-kv.vault.azure.net", "")
	if c1 != c2 {
		t.Error("clients should be cached per vault and tenant")
	}
	if _, ok := c1.(*azsecrets.Client); !ok {
		t.Errorf("client type = %T", c1)
	}
}
