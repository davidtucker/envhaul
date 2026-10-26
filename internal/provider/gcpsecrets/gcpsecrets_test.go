package gcpsecrets

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"hash/crc32"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"golang.org/x/oauth2"

	"github.com/davidtucker/envhaul/internal/config"
	"github.com/davidtucker/envhaul/internal/provider"
	"github.com/davidtucker/envhaul/internal/template"
)

type fakeGCP struct {
	mu       sync.Mutex
	secrets  map[string]string // full version name -> value
	tokens   map[string]bool   // accepted bearer tokens
	requests []string
	badCRC   bool
}

func (f *fakeGCP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.requests = append(f.requests, r.Method+" "+r.URL.Path)
	f.mu.Unlock()
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if strings.Contains(r.URL.Path, ":generateAccessToken") {
		if strings.Contains(r.URL.Path, "denied@") {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if strings.Contains(r.URL.Path, "empty@") {
			_, _ = w.Write([]byte(`{}`))
			return
		}
		if strings.Contains(r.URL.Path, "broken@") {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"accessToken": "impersonated"})
		return
	}
	if !f.tokens[token] {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/"), ":access")
	if strings.Contains(name, "/secrets/forbidden/") {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"status":"PERMISSION_DENIED","message":"denied"}}`))
		return
	}
	if strings.Contains(name, "/secrets/garbage/") {
		_, _ = w.Write([]byte("<html>proxy error</html>"))
		return
	}
	if strings.Contains(name, "/secrets/disabled/") {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"status":"FAILED_PRECONDITION","message":"Secret version is in DISABLED state."}}`))
		return
	}
	v, ok := f.secrets[name]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"status":"NOT_FOUND"}}`))
		return
	}
	crc := crc32.Checksum([]byte(v), crc32.MakeTable(crc32.Castagnoli))
	if f.badCRC {
		crc++
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"name":    name,
		"payload": map[string]string{"data": base64.StdEncoding.EncodeToString([]byte(v)), "dataCrc32c": strconv.FormatUint(uint64(crc), 10)},
	})
}

func setup(t *testing.T, src *config.Source, envVars map[string]string, credProject string) (*fakeGCP, *Provider) {
	t.Helper()
	f := &fakeGCP{
		tokens: map[string]bool{"adc": true, "impersonated": true},
		secrets: map[string]string{
			"projects/dev/secrets/db-password/versions/latest":  "dev-pass",
			"projects/dev/secrets/db-password/versions/3":       "old-pass",
			"projects/prod/secrets/db-password/versions/latest": "prod-pass",
			"projects/dev/secrets/json/versions/latest":         `{"user":"u"}`,
		},
	}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	p := New(Options{
		Source: src, Online: true, Endpoint: srv.URL, IAMEndpoint: srv.URL,
		Getenv: func(k string) string { return envVars[k] },
		Credentials: func(context.Context) (oauth2.TokenSource, string, error) {
			return oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "adc"}), credProject, nil
		},
	})
	return f, p
}

func ref(t *testing.T, s string) *template.Ref {
	t.Helper()
	tmpl, err := template.Parse("{{" + s + "}}")
	if err != nil {
		t.Fatal(err)
	}
	return tmpl.Refs()[0]
}

func fetch(t *testing.T, p *Provider, refs ...string) (map[string]provider.Result, error) {
	t.Helper()
	var reqs []provider.Request
	for _, s := range refs {
		reqs = append(reqs, p.Request(ref(t, s)))
	}
	res, err := p.Fetch(context.Background(), reqs)
	if err != nil {
		return nil, err
	}
	out := map[string]provider.Result{}
	for i, s := range refs {
		out[s] = res[reqs[i]]
	}
	return out, nil
}

func TestValidateAndRequest(t *testing.T) {
	src := &config.Source{Settings: map[string]string{"project": "dev"}, Profiles: map[string]map[string]string{"prod": {"project": "prod-1"}}}
	p := New(Options{Source: src})
	tests := []struct{ in, want string }{
		{"gcp:db-password", "projects/dev/secrets/db-password/versions/latest"},
		{"gcp:db-password:3", "projects/dev/secrets/db-password/versions/3"},
		{"gcp:db-password@prod", "projects/prod-1/secrets/db-password/versions/latest"},
		{"gcp:db-password@other-project", "projects/other-project/secrets/db-password/versions/latest"},
		{"gcp:projects/x/secrets/s/versions/2", "projects/x/secrets/s/versions/2"},
		{"gcp:projects/x/secrets/s", "projects/x/secrets/s/versions/latest"},
	}
	for _, tt := range tests {
		r := ref(t, tt.in)
		if err := p.Validate(r); err != nil {
			t.Errorf("Validate(%s): %v", tt.in, err)
		}
		if got := p.Request(r).Path; got != tt.want {
			t.Errorf("Request(%s) = %s, want %s", tt.in, got, tt.want)
		}
	}
	if got := New(Options{}).Request(ref(t, "gcp:db")).Path; got != "secrets/db/versions/latest" {
		t.Errorf("without a project, Path = %s", got)
	}
	for _, bad := range []string{"gcp:bad.name", "gcp:projects/x/secret/s", "gcp:db:v.1", "gcp:projects/x/secrets/s/versions/a.b"} {
		if err := p.Validate(ref(t, bad)); err == nil {
			t.Errorf("Validate(%s) should fail", bad)
		}
	}
	if p.Scheme() != "gcp" || p.Description() != "Google Cloud Secret Manager" {
		t.Error("identity")
	}
}

func TestFetch(t *testing.T) {
	_, p := setup(t, &config.Source{Settings: map[string]string{"project": "dev"}}, nil, "")
	got, err := fetch(t, p, "gcp:db-password", "gcp:db-password:3", "gcp:db-password@prod", "gcp:json", "gcp:missing", "gcp:projects/dev/secrets/db-password/versions/3")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"gcp:db-password":      "dev-pass",
		"gcp:db-password:3":    "old-pass",
		"gcp:db-password@prod": "prod-pass",
		"gcp:json":             `{"user":"u"}`,
		"gcp:projects/dev/secrets/db-password/versions/3": "old-pass",
	}
	for s, w := range want {
		if r := got[s]; !r.Found || r.Value != w {
			t.Errorf("%s = %+v, want %q", s, r, w)
		}
	}
	if got["gcp:missing"].Found {
		t.Error("a missing secret should not be found")
	}
}

func TestProjectFallbacks(t *testing.T) {
	// GOOGLE_CLOUD_PROJECT, then the credentials' project.
	_, p := setup(t, nil, map[string]string{"GOOGLE_CLOUD_PROJECT": "dev"}, "prod")
	if got, err := fetch(t, p, "gcp:db-password"); err != nil || got["gcp:db-password"].Value != "dev-pass" {
		t.Errorf("GOOGLE_CLOUD_PROJECT: %v %v", got, err)
	}
	_, p = setup(t, nil, nil, "prod")
	if got, err := fetch(t, p, "gcp:db-password"); err != nil || got["gcp:db-password"].Value != "prod-pass" {
		t.Errorf("credentials project: %v %v", got, err)
	}
	_, p = setup(t, nil, nil, "")
	if _, err := fetch(t, p, "gcp:db-password"); err == nil || !strings.Contains(err.Error(), "no Google Cloud project") {
		t.Errorf("no project: err = %v", err)
	}
}

func TestImpersonation(t *testing.T) {
	src := &config.Source{Settings: map[string]string{"project": "dev"}, Profiles: map[string]map[string]string{
		"sa":     {"impersonate_service_account": "reader@dev.iam.gserviceaccount.com"},
		"denied": {"impersonate_service_account": "denied@dev.iam.gserviceaccount.com"},
	}}
	f, p := setup(t, src, nil, "")
	if got, err := fetch(t, p, "gcp:db-password@sa"); err != nil || got["gcp:db-password@sa"].Value != "dev-pass" {
		t.Fatalf("impersonated read: %v %v", got, err)
	}
	if !strings.Contains(strings.Join(f.requests, " "), "reader@dev.iam.gserviceaccount.com:generateAccessToken") {
		t.Errorf("no impersonation call in %v", f.requests)
	}
	if _, err := fetch(t, p, "gcp:db-password@denied"); err == nil || !strings.Contains(err.Error(), "roles/iam.serviceAccountTokenCreator") {
		t.Errorf("denied impersonation: err = %v", err)
	}
}

func TestFetchErrors(t *testing.T) {
	src := &config.Source{Settings: map[string]string{"project": "dev"}}
	for _, tt := range []struct{ ref, want string }{
		{"gcp:forbidden", "roles/secretmanager.secretAccessor"},
		{"gcp:disabled", "FAILED_PRECONDITION: Secret version is in DISABLED state."},
	} {
		_, p := setup(t, src, nil, "")
		if _, err := fetch(t, p, tt.ref); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: err = %v, want %q", tt.ref, err, tt.want)
		}
	}

	f, p := setup(t, src, nil, "")
	f.badCRC = true
	if _, err := fetch(t, p, "gcp:db-password"); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("bad CRC: err = %v", err)
	}

	f, p = setup(t, src, nil, "")
	f.tokens = map[string]bool{}
	if _, err := fetch(t, p, "gcp:db-password"); err == nil || !strings.Contains(err.Error(), "credentials rejected") {
		t.Errorf("rejected token: err = %v", err)
	}

	noCreds := New(Options{Source: src, Online: true, Credentials: func(context.Context) (oauth2.TokenSource, string, error) {
		return nil, "", errors.New("could not find default credentials")
	}})
	if _, err := fetch(t, noCreds, "gcp:x"); err == nil || !strings.Contains(err.Error(), "gcloud auth application-default login") {
		t.Errorf("no credentials: err = %v", err)
	}

	badToken := New(Options{Source: src, Online: true, Credentials: func(context.Context) (oauth2.TokenSource, string, error) {
		return oauth2.ReuseTokenSource(nil, errSource{}), "", nil
	}})
	if _, err := fetch(t, badToken, "gcp:x"); err == nil || !strings.Contains(err.Error(), "can't be used") {
		t.Errorf("token error: err = %v", err)
	}

	unreachable := New(Options{Source: src, Online: true, Endpoint: "http://127.0.0.1:1", Credentials: func(context.Context) (oauth2.TokenSource, string, error) {
		return oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "adc"}), "", nil
	}})
	if _, err := fetch(t, unreachable, "gcp:x"); err == nil || !strings.Contains(err.Error(), "accessing projects/dev/secrets/x/versions/latest") {
		t.Errorf("unreachable: err = %v", err)
	}

	if _, err := New(Options{}).Fetch(context.Background(), nil); err == nil {
		t.Error("offline provider should refuse to fetch")
	}
}

type errSource struct{}

func (errSource) Token() (*oauth2.Token, error) { return nil, errors.New("refresh token expired") }

func TestImpersonationResponseErrors(t *testing.T) {
	src := &config.Source{Settings: map[string]string{"project": "dev"}, Profiles: map[string]map[string]string{
		"empty":  {"impersonate_service_account": "empty@dev.iam.gserviceaccount.com"},
		"broken": {"impersonate_service_account": "broken@dev.iam.gserviceaccount.com"},
	}}
	for profile, want := range map[string]string{
		"empty":  "impersonating empty@dev.iam.gserviceaccount.com: HTTP 200",
		"broken": "impersonating broken@dev.iam.gserviceaccount.com: HTTP 500",
	} {
		_, p := setup(t, src, nil, "")
		if _, err := fetch(t, p, "gcp:db-password@"+profile); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want %q", profile, err, want)
		}
	}
}

func TestUnexpectedResponse(t *testing.T) {
	_, p := setup(t, &config.Source{Settings: map[string]string{"project": "dev"}}, nil, "")
	if _, err := fetch(t, p, "gcp:garbage"); err == nil || !strings.Contains(err.Error(), "unexpected response") {
		t.Errorf("non-JSON 200: err = %v", err)
	}
}

func TestRequestKeepsInvalidPath(t *testing.T) {
	// Validate rejects this; Request must not panic or invent a name.
	if got := New(Options{}).Request(ref(t, "gcp:bad.name")); got.Path != "bad.name" {
		t.Errorf("Request = %+v", got)
	}
}

// TestApplicationDefaultFromFile loads real Application Default Credentials
// from a service account key file, which needs no network access.
func TestApplicationDefaultFromFile(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	creds, _ := json.Marshal(map[string]string{
		"type":           "service_account",
		"project_id":     "adc-project",
		"private_key_id": "abc123",
		"private_key":    string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})),
		"client_email":   "reader@adc-project.iam.gserviceaccount.com",
		"client_id":      "1",
		"token_uri":      "https://oauth2.googleapis.com/token",
	})
	path := filepath.Join(t.TempDir(), "sa.json")
	if err := os.WriteFile(path, creds, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", path)

	ts, project, err := applicationDefault(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if project != "adc-project" || ts == nil {
		t.Errorf("project = %q, token source nil = %v", project, ts == nil)
	}

	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", filepath.Join(t.TempDir(), "missing.json"))
	if _, _, err := applicationDefault(context.Background()); err == nil {
		t.Error("a missing credentials file should be an error")
	}
}
