package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/davidtucker/envhaul/internal/template"
)

func parse(t *testing.T, src string, opts Options) (*Config, error) {
	t.Helper()
	return Parse(filepath.Join(t.TempDir(), "envhaul.yaml"), []byte(src), opts)
}

func mustParse(t *testing.T, src string) *Config {
	t.Helper()
	cfg, err := parse(t, src, Options{})
	if err != nil {
		t.Fatalf("unexpected error:\n%v", err)
	}
	return cfg
}

const sample = `
name: dev
description: Development
aws_profile: acme-dev
aws_region: us-east-1
ssm_prefix: /app/dev
components:
  api:
    env_file: containers/api/.env
    vars:
      ENV: dev
      ZED: "true"
      VERSION: 1.10
      OCTAL: 0755
      YES_NO: yes
      EMPTY: ""
      NULLED:
      KEY: "{{ssm:api/KEY@shared}}"
  frontend:
    env_file: frontend/.env
    vars:
      URL: "http://localhost:8000"
`

func TestParseSample(t *testing.T) {
	cfg := mustParse(t, sample)
	if cfg.Name != "dev" || cfg.AWSProfile != "acme-dev" || cfg.AWSRegion != "us-east-1" || cfg.SSMPrefix != "/app/dev" {
		t.Errorf("unexpected top-level fields: %+v", cfg)
	}
	if cfg.Version != CurrentVersion {
		t.Errorf("Version = %d, want default %d", cfg.Version, CurrentVersion)
	}
	if got := strings.Join(cfg.ComponentNames(), ","); got != "api,frontend" {
		t.Errorf("components = %s, want file order api,frontend", got)
	}
	api := cfg.Component("api")
	var names, values []string
	for _, v := range api.Vars {
		names = append(names, v.Name)
		values = append(values, v.Raw)
	}
	if got := strings.Join(names, ","); got != "ENV,ZED,VERSION,OCTAL,YES_NO,EMPTY,NULLED,KEY" {
		t.Errorf("var order = %s", got)
	}
	// Scalars must be kept exactly as written, not YAML-normalized.
	want := []string{"dev", "true", "1.10", "0755", "yes", "", "", "{{ssm:api/KEY@shared}}"}
	if strings.Join(values, "|") != strings.Join(want, "|") {
		t.Errorf("values = %q, want %q", values, want)
	}
	if got := cfg.EnvFilePath(api); got != filepath.Join(cfg.Dir(), "containers", "api", ".env") {
		t.Errorf("EnvFilePath = %s", got)
	}
	if api.Var("KEY").Template.IsLiteral() {
		t.Error("KEY should contain a reference")
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name, src, want string
	}{
		{"empty", "", "empty"},
		{"not a mapping", "- a\n- b\n", "top level must be a mapping"},
		{"invalid yaml", "a: [", "invalid YAML"},
		{"missing components", "name: x\n", `missing required key "components"`},
		{"unknown top key", "naem: x\ncomponents:\n  a:\n    env_file: .env\n    vars: {}\n", `:1:1: unknown key "naem"`},
		{"unknown component key", "components:\n  a:\n    env_file: .env\n    vars: {}\n    envfile: x\n", `unknown key "envfile"`},
		{"missing env_file", "components:\n  a:\n    vars: {}\n", "missing env_file"},
		{"missing vars", "components:\n  a:\n    env_file: .env\n", "missing vars"},
		{"bad var name", "components:\n  a:\n    env_file: .env\n    vars:\n      1BAD: x\n", "invalid variable name"},
		{"dashed var name", "components:\n  a:\n    env_file: .env\n    vars:\n      MY-VAR: x\n", "invalid variable name"},
		{"duplicate var", "components:\n  a:\n    env_file: .env\n    vars:\n      A: x\n      A: y\n", `duplicate key "A"`},
		{"list value", "components:\n  a:\n    env_file: .env\n    vars:\n      A: [1]\n", "must be a string, number or boolean, not a list"},
		{"bad template", "components:\n  a:\n    env_file: .env\n    vars:\n      A: \"x{{ssm:x\"\n", "unterminated"},
		{"bad pipe", "components:\n  a:\n    env_file: .env\n    vars:\n      A: \"{{ssm:x|nope}}\"\n", `unknown pipe function "nope"`},
		{"future version", "version: 9\ncomponents:\n  a:\n    env_file: .env\n    vars: {}\n", "newer than this envhaul supports"},
		{"bad version", "version: one\ncomponents:\n  a:\n    env_file: .env\n    vars: {}\n", "positive integer"},
		{"relative ssm prefix", "ssm_prefix: app/dev\ncomponents:\n  a:\n    env_file: .env\n    vars: {}\n", "must start with /"},
		{"duplicate env file", "components:\n  a:\n    env_file: x/.env\n    vars: {}\n  b:\n    env_file: ./x/.env\n    vars: {}\n", "writes the same env_file"},
		{"no components", "components: {}\n", "at least one component"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parse(t, tt.src, Options{})
			if err == nil {
				t.Fatalf("expected error containing %q", tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %q\nwant it to contain %q", err, tt.want)
			}
		})
	}
}

func TestParseReportsAllErrors(t *testing.T) {
	src := "components:\n  a:\n    env_file: .env\n    vars:\n      1A: x\n      B: \"{{ssm:x|nope}}\"\n      C: [1]\n"
	_, err := parse(t, src, Options{})
	var diags Diagnostics
	if !errors.As(err, &diags) {
		t.Fatalf("error type %T, want Diagnostics", err)
	}
	if len(diags) != 3 {
		t.Errorf("got %d diagnostics, want 3:\n%v", len(diags), err)
	}
}

func TestDiagnosticColumn(t *testing.T) {
	src := "components:\n  a:\n    env_file: .env\n    vars:\n      A: \"xx{{ssm:x|nope}}\"\n"
	_, err := parse(t, src, Options{})
	// The value's opening quote is at column 10; the reference starts 2 bytes
	// into the string.
	if err == nil || !strings.Contains(err.Error(), ":5:13:") {
		t.Errorf("error = %v, want position 5:13", err)
	}
}

func TestValidateRefOption(t *testing.T) {
	src := "components:\n  a:\n    env_file: .env\n    vars:\n      A: \"{{vault:x}}\"\n"
	_, err := parse(t, src, Options{ValidateRef: func(r *template.Ref) error {
		if r.Scheme != "ssm" {
			return errors.New("unknown scheme " + r.Scheme)
		}
		return nil
	}})
	if err == nil || !strings.Contains(err.Error(), "unknown scheme vault") {
		t.Errorf("error = %v", err)
	}
}

func TestSelect(t *testing.T) {
	cfg := mustParse(t, sample)
	got, err := cfg.Select([]string{"frontend", "api"})
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Name != "api" || got[1].Name != "frontend" {
		t.Errorf("Select should keep file order, got %s,%s", got[0].Name, got[1].Name)
	}
	if _, err := cfg.Select([]string{"nope"}); err == nil || !strings.Contains(err.Error(), "available: api, frontend") {
		t.Errorf("Select(nope) error = %v", err)
	}
	all, _ := cfg.Select(nil)
	if len(all) != 2 {
		t.Errorf("Select(nil) = %d components", len(all))
	}
}

func TestDiscover(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	deep := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Discover(deep); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Discover with no config: err = %v", err)
	}
	cfgPath := filepath.Join(root, "envhaul.yaml")
	if err := os.WriteFile(cfgPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Discover(deep)
	if err != nil || got != cfgPath {
		t.Fatalf("Discover = %q, %v; want %q", got, err, cfgPath)
	}
	// A config in a nested directory wins over the root one.
	nested := filepath.Join(root, "a", ".envhaul.yaml")
	if err := os.WriteFile(nested, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, _ := Discover(deep); got != nested {
		t.Errorf("Discover = %q, want nearest %q", got, nested)
	}
}

func TestDiscoverStopsAtRepoRoot(t *testing.T) {
	outer := t.TempDir()
	if err := os.WriteFile(filepath.Join(outer, "envhaul.yaml"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(outer, "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Discover(repo); !errors.Is(err, ErrNotFound) {
		t.Errorf("Discover should not escape the repository, err = %v", err)
	}
}
