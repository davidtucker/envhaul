package resolve

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/davidtucker/envhaul/internal/config"
	"github.com/davidtucker/envhaul/internal/provider"
	"github.com/davidtucker/envhaul/internal/template"
)

// fakeProvider serves values from a map keyed by "profile|path" and records
// every request it receives.
type fakeProvider struct {
	scheme  string
	prefix  string
	values  map[string]string
	err     error
	mu      sync.Mutex
	fetched []provider.Request
}

func (f *fakeProvider) Scheme() string               { return f.scheme }
func (f *fakeProvider) Description() string          { return "fake" }
func (f *fakeProvider) Validate(*template.Ref) error { return nil }
func (f *fakeProvider) Request(ref *template.Ref) provider.Request {
	profile := ref.Profile
	if profile == "" {
		profile = "default"
	}
	return provider.Request{Path: f.prefix + ref.Path, Profile: profile}
}

func (f *fakeProvider) Fetch(_ context.Context, reqs []provider.Request) (map[provider.Request]provider.Result, error) {
	f.mu.Lock()
	f.fetched = append(f.fetched, reqs...)
	f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	out := map[provider.Request]provider.Result{}
	for _, r := range reqs {
		v, ok := f.values[r.Profile+"|"+r.Path]
		out[r] = provider.Result{Value: v, Found: ok}
	}
	return out, nil
}

func load(t *testing.T, vars string) *config.Config {
	t.Helper()
	src := "components:\n  app:\n    env_file: .env\n    vars:\n" + vars
	cfg, err := config.Parse(filepath.Join(t.TempDir(), "envhaul.yaml"), []byte(src), config.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func setup() (*fakeProvider, *fakeProvider, *provider.Registry) {
	ssm := &fakeProvider{scheme: "ssm", prefix: "/dev/", values: map[string]string{
		"default|/dev/api/KEY":     "k-123",
		"shared|/dev/api/SENTRY":   "https://sentry",
		"default|/dev/json":        `{"n": 3, "b": true, "o": {"x": 1}, "nil": null}`,
		"default|/dev/padded":      "  spaced  ",
		"default|/dev/empty-value": "",
	}}
	sec := &fakeProvider{scheme: "secret", values: map[string]string{
		"default|db": `{"user":"admin","pass":"p@ss:w/rd"}`,
	}}
	return ssm, sec, provider.NewRegistry(ssm, sec)
}

func TestResolve(t *testing.T) {
	ssm, sec, reg := setup()
	cfg := load(t, `
      INLINE: plain
      KEY: "{{ssm:api/KEY}}"
      S1: "{{ssm:api/SENTRY@shared}}"
      S2: "{{ssm:api/SENTRY@shared}}"
      URL: "postgresql://{{secret:db#user}}:{{secret:db#pass|urlencode}}@host/db"
      NUM: "{{ssm:json#n}}"
      BOOL: "{{ssm:json#b}}"
      OBJ: "{{ssm:json#o}}"
      NIL: "{{ssm:json#nil}}"
      TRIM: "{{ssm:padded|trim|upper}}"
      DEF: "{{ssm:nope|default:fallback}}"
      EMPTYDEF: "{{ssm:empty-value|default:x}}"
      ESC: 'a\{{b'
`)
	r := &Resolver{Registry: reg}
	comps, missing, err := r.Resolve(context.Background(), cfg.Components)
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 0 {
		t.Errorf("unexpected missing: %v", missing)
	}
	want := map[string]string{
		"INLINE":   "plain",
		"KEY":      "k-123",
		"S1":       "https://sentry",
		"S2":       "https://sentry",
		"URL":      "postgresql://admin:p%40ss%3Aw%2Frd@host/db",
		"NUM":      "3",
		"BOOL":     "true",
		"OBJ":      `{"x":1}`,
		"NIL":      "",
		"TRIM":     "SPACED",
		"DEF":      "fallback",
		"EMPTYDEF": "x",
		"ESC":      "a{{b",
	}
	got := comps[0]
	for name, w := range want {
		if v, _ := got.Lookup(name); v != w {
			t.Errorf("%s = %q, want %q", name, v, w)
		}
	}
	if got.Values[0].Name != "INLINE" || got.Values[len(got.Values)-1].Name != "ESC" {
		t.Error("values are not in config order")
	}

	// Each unique value is fetched exactly once: api/KEY, api/SENTRY@shared,
	// json, padded, nope, empty-value for ssm; db (both keys) for secret.
	if len(ssm.fetched) != 6 {
		t.Errorf("ssm fetched %d requests, want 6: %v", len(ssm.fetched), ssm.fetched)
	}
	if len(sec.fetched) != 1 {
		t.Errorf("secret fetched %d requests, want 1: %v", len(sec.fetched), sec.fetched)
	}
}

func TestResolveMissing(t *testing.T) {
	_, _, reg := setup()
	cfg := load(t, `
      A: "{{ssm:absent}}"
      B: "prefix-{{secret:db#nokey}}"
      C: "{{ssm:absent|trim}}"
`)
	r := &Resolver{Registry: reg}
	comps, missing, err := r.Resolve(context.Background(), cfg.Components)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := comps[0].Lookup("B"); v != "prefix-" {
		t.Errorf("B = %q, want missing part rendered empty", v)
	}
	if len(missing) != 3 {
		t.Fatalf("missing = %v, want 3 entries", missing)
	}
	if missing[0].Source != "ssm:/dev/absent@default" {
		t.Errorf("missing[0].Source = %q", missing[0].Source)
	}
	if !strings.Contains(missing[1].String(), `key "nokey" not present`) {
		t.Errorf("missing[1] = %q", missing[1].String())
	}

	r.Strict = true
	_, _, err = r.Resolve(context.Background(), cfg.Components)
	var me *MissingError
	if !errors.As(err, &me) || len(me.Missing) != 3 {
		t.Fatalf("strict error = %v", err)
	}
	if !strings.Contains(err.Error(), "app.A: ssm:/dev/absent@default not found") {
		t.Errorf("strict error message:\n%s", err)
	}
}

func TestResolveErrors(t *testing.T) {
	ssm, _, reg := setup()
	cfg := load(t, `      A: "{{ssm:api/KEY#field}}"`+"\n")
	r := &Resolver{Registry: reg}
	if _, _, err := r.Resolve(context.Background(), cfg.Components); err == nil || !strings.Contains(err.Error(), "not a JSON object") {
		t.Errorf("err = %v, want JSON object error", err)
	}

	ssm.err = errors.New("access denied")
	cfg = load(t, `      A: "{{ssm:api/KEY}}"`+"\n")
	if _, _, err := r.Resolve(context.Background(), cfg.Components); err == nil || !strings.Contains(err.Error(), "access denied") {
		t.Errorf("err = %v, want provider error", err)
	}

	cfg = load(t, `      A: "{{vault:x}}"`+"\n")
	if _, _, err := r.Resolve(context.Background(), cfg.Components); err == nil || !strings.Contains(err.Error(), `unknown source "vault"`) {
		t.Errorf("err = %v, want unknown source", err)
	}
}

func TestResolveNoRefsSkipsFetch(t *testing.T) {
	ssm, _, reg := setup()
	cfg := load(t, "      A: plain\n")
	if _, _, err := (&Resolver{Registry: reg}).Resolve(context.Background(), cfg.Components); err != nil {
		t.Fatal(err)
	}
	if len(ssm.fetched) != 0 {
		t.Error("providers should not be called when there are no references")
	}
}
