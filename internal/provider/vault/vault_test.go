package vault

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/davidtucker/envhaul/internal/config"
	"github.com/davidtucker/envhaul/internal/provider"
	"github.com/davidtucker/envhaul/internal/template"
)

// fakeVault serves KV v1 and v2 reads for secrets keyed by
// "namespace|/v1/<api path>[?version=N]".
type fakeVault struct {
	mu       sync.Mutex
	secrets  map[string]map[string]any
	token    string
	requests []string
}

func (f *fakeVault) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get("X-Vault-Namespace") + "|" + r.URL.Path
	if r.URL.RawQuery != "" {
		key += "?" + r.URL.RawQuery
	}
	f.mu.Lock()
	f.requests = append(f.requests, key)
	f.mu.Unlock()
	if r.Header.Get("X-Vault-Token") != f.token {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"errors":["permission denied"]}`))
		return
	}
	data, ok := f.secrets[key]
	switch {
	case strings.HasSuffix(r.URL.Path, "/boom"):
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"errors":["storage is sealed"]}`))
	case !ok:
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errors":[]}`))
	case strings.Contains(r.URL.Path, "/data/"):
		if data == nil {
			// A deleted KV v2 version: 200 with null data in some servers.
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"data": nil}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"data": data, "metadata": map[string]any{"version": 1}}})
	default:
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}
}

func newServer(t *testing.T) (*fakeVault, *httptest.Server) {
	t.Helper()
	f := &fakeVault{token: "root", secrets: map[string]map[string]any{
		"|/v1/secret/data/app/db":           {"user": "admin", "password": "p@ss"},
		"|/v1/secret/data/app/db?version=2": {"user": "admin", "password": "older"},
		"|/v1/kv/data/team/api":             {"key": "k-123"},
		"team|/v1/secret/data/app/db":       {"user": "team-admin"},
		"|/v1/team/kv/data/nested":          {"v": "nested"},
		"|/v1/legacy/app":                   {"token": "v1-value"},
		"|/v1/secret/data/deleted":          nil,
	}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, srv
}

func ref(t *testing.T, s string) *template.Ref {
	t.Helper()
	tmpl, err := template.Parse("{{" + s + "}}")
	if err != nil {
		t.Fatal(err)
	}
	return tmpl.Refs()[0]
}

func env(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

func noHome() (string, error) { return "", os.ErrNotExist }

func TestRequestAndValidate(t *testing.T) {
	src := &config.Source{Settings: map[string]string{"mount": "kv"}, Profiles: map[string]map[string]string{"nested": {"mount": "/team/kv/"}}}
	p := New(Options{Source: src})
	tests := []struct {
		in   string
		want provider.Request
	}{
		{"vault:app/db", provider.Request{Path: "kv/app/db", Base: "kv"}},
		{"vault:app/db:3", provider.Request{Path: "kv/app/db:3", Base: "kv"}},
		{"vault:/secret/app/db", provider.Request{Path: "secret/app/db", Base: "secret"}},
		{"vault:app/db@prod", provider.Request{Path: "kv/app/db", Profile: "prod", Base: "kv"}},
		{"vault:x@nested", provider.Request{Path: "team/kv/x", Profile: "nested", Base: "team/kv"}},
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
	if got := New(Options{}).Request(ref(t, "vault:app/db")); got.Base != "secret" {
		t.Errorf("default mount = %q, want secret", got.Base)
	}

	for _, bad := range []string{"vault:/kv", "vault:/kv/", "vault:app/", "vault:a//b"} {
		if err := p.Validate(ref(t, bad)); err == nil {
			t.Errorf("Validate(%s) should fail", bad)
		}
	}
	if p.Scheme() != "vault" || p.Description() != "HashiCorp Vault / OpenBao" {
		t.Error("identity")
	}
}

func fetch(t *testing.T, p *Provider, refs ...string) (map[string]provider.Result, error) {
	t.Helper()
	var reqs []provider.Request
	byRef := map[string]provider.Request{}
	for _, s := range refs {
		r := p.Request(ref(t, s))
		reqs = append(reqs, r)
		byRef[s] = r
	}
	res, err := p.Fetch(context.Background(), reqs)
	if err != nil {
		return nil, err
	}
	out := map[string]provider.Result{}
	for s, r := range byRef {
		out[s] = res[r]
	}
	return out, nil
}

func TestFetch(t *testing.T) {
	fake, srv := newServer(t)
	src := &config.Source{
		Settings: map[string]string{},
		Profiles: map[string]map[string]string{
			"kv":     {"mount": "kv"},
			"legacy": {"mount": "legacy", "kv_version": "1"},
			"nested": {"mount": "team/kv"},
		},
	}
	p := New(Options{Source: src, Online: true, Getenv: env(map[string]string{"VAULT_ADDR": srv.URL, "VAULT_TOKEN": "root"}), HomeDir: noHome})
	got, err := fetch(t, p,
		"vault:app/db", "vault:app/db:2", "vault:/secret/app/db", "vault:team/api@kv",
		"vault:app/db@team", "vault:nested@nested", "vault:app@legacy", "vault:missing", "vault:deleted")
	if err != nil {
		t.Fatal(err)
	}
	checks := map[string]string{
		"vault:app/db":         `{"password":"p@ss","user":"admin"}`,
		"vault:app/db:2":       `{"password":"older","user":"admin"}`,
		"vault:/secret/app/db": `{"password":"p@ss","user":"admin"}`,
		"vault:team/api@kv":    `{"key":"k-123"}`,
		"vault:app/db@team":    `{"user":"team-admin"}`,
		"vault:nested@nested":  `{"v":"nested"}`,
		"vault:app@legacy":     `{"token":"v1-value"}`,
	}
	for s, want := range checks {
		if r := got[s]; !r.Found || r.Value != want {
			t.Errorf("%s = %+v, want %s", s, r, want)
		}
	}
	for _, s := range []string{"vault:missing", "vault:deleted"} {
		if got[s].Found {
			t.Errorf("%s should be missing", s)
		}
	}
	// The same secret with and without an explicit mount is one request, so
	// the resolver fetches it once.
	if p.Request(ref(t, "vault:app/db")) != p.Request(ref(t, "vault:/secret/app/db")) {
		t.Error("vault:app/db and vault:/secret/app/db should be the same request")
	}
	// A namespace is sent as a header, never in the path.
	for _, r := range fake.requests {
		if strings.HasPrefix(r, "team|") && r != "team|/v1/secret/data/app/db" {
			t.Errorf("unexpected namespaced request %q", r)
		}
	}
}

func TestTokenSources(t *testing.T) {
	_, srv := newServer(t)
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, ".vault-token"), []byte("root\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	homeDir := func() (string, error) { return home, nil }

	// ~/.vault-token, as `vault login` writes it.
	p := New(Options{Online: true, Getenv: env(map[string]string{"VAULT_ADDR": srv.URL}), HomeDir: homeDir})
	if got, err := fetch(t, p, "vault:app/db"); err != nil || !got["vault:app/db"].Found {
		t.Errorf("~/.vault-token: %v %v", got, err)
	}

	// token_env on a profile.
	src := &config.Source{Profiles: map[string]map[string]string{"ci": {"address": srv.URL, "token_env": "CI_VAULT"}}}
	p = New(Options{Source: src, Online: true, Getenv: env(map[string]string{"CI_VAULT": "root"}), HomeDir: noHome})
	if got, err := fetch(t, p, "vault:app/db@ci"); err != nil || !got["vault:app/db@ci"].Found {
		t.Errorf("token_env: %v %v", got, err)
	}
}

func TestFetchErrors(t *testing.T) {
	_, srv := newServer(t)
	tests := []struct {
		name string
		opts Options
		ref  string
		want string
	}{
		{"offline", Options{}, "vault:x", "not configured for remote access"},
		{"no address", Options{Online: true, Getenv: env(nil), HomeDir: noHome}, "vault:x", "no Vault address"},
		{"no token", Options{Online: true, Getenv: env(map[string]string{"VAULT_ADDR": srv.URL}), HomeDir: noHome}, "vault:x", "run `vault login`"},
		{"empty token_env", Options{Online: true, Source: &config.Source{Settings: map[string]string{"token_env": "NOPE"}}, Getenv: env(map[string]string{"VAULT_ADDR": srv.URL}), HomeDir: noHome}, "vault:x", "NOPE (vault token_env) is empty"},
		{"bad token", Options{Online: true, Getenv: env(map[string]string{"VAULT_ADDR": srv.URL, "VAULT_TOKEN": "wrong"}), HomeDir: noHome}, "vault:app/db", "permission denied reading secret/app/db"},
		{"server error", Options{Online: true, Getenv: env(map[string]string{"VAULT_ADDR": srv.URL, "VAULT_TOKEN": "root"}), HomeDir: noHome}, "vault:boom", "500 Internal Server Error: storage is sealed"},
		{"unreachable", Options{Online: true, Getenv: env(map[string]string{"VAULT_ADDR": "http://127.0.0.1:1", "VAULT_TOKEN": "root"}), HomeDir: noHome}, "vault:x", "can't reach http://127.0.0.1:1"},
		{"v1 with version", Options{Online: true, Source: &config.Source{Settings: map[string]string{"kv_version": "1"}}, Getenv: env(map[string]string{"VAULT_ADDR": srv.URL, "VAULT_TOKEN": "root"}), HomeDir: noHome}, "vault:x:2", "versions need the KV v2"},
		{"bad CA file", Options{Online: true, Getenv: env(map[string]string{"VAULT_ADDR": srv.URL, "VAULT_TOKEN": "root", "VAULT_CACERT": "/nonexistent.pem"}), HomeDir: noHome}, "vault:x", "reading VAULT_CACERT"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := fetch(t, New(tt.opts), tt.ref)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestCustomCA(t *testing.T) {
	f := &fakeVault{token: "root", secrets: map[string]map[string]any{"|/v1/secret/data/tls": {"ok": "yes"}}}
	srv := httptest.NewTLSServer(f)
	defer srv.Close()

	// Without the CA the TLS handshake fails.
	vars := map[string]string{"VAULT_ADDR": srv.URL, "VAULT_TOKEN": "root"}
	if _, err := fetch(t, New(Options{Online: true, Getenv: env(vars), HomeDir: noHome}), "vault:tls"); err == nil {
		t.Fatal("expected a certificate error without VAULT_CACERT")
	}

	ca := filepath.Join(t.TempDir(), "ca.pem")
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	if err := os.WriteFile(ca, pemBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	vars["VAULT_CACERT"] = ca
	got, err := fetch(t, New(Options{Online: true, Getenv: env(vars), HomeDir: noHome}), "vault:tls")
	if err != nil || got["vault:tls"].Value != `{"ok":"yes"}` {
		t.Errorf("with VAULT_CACERT: %v %v", got, err)
	}

	notPEM := filepath.Join(t.TempDir(), "x.pem")
	_ = os.WriteFile(notPEM, []byte("not a cert"), 0o600)
	vars["VAULT_CACERT"] = notPEM
	if _, err := fetch(t, New(Options{Online: true, Getenv: env(vars), HomeDir: noHome}), "vault:tls"); err == nil || !strings.Contains(err.Error(), "no PEM certificates") {
		t.Errorf("err = %v", err)
	}
}
