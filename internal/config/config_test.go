package config

import (
	"errors"
	"io/fs"
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
	_, err := parse(t, src, Options{ValidateRef: func(_ *Config, r *template.Ref) error {
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

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "envhaul.yaml")
	if err := os.WriteFile(path, []byte(sample), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Path != path || cfg.Dir() != dir {
		t.Errorf("Path=%q Dir=%q, want the absolute file and its directory", cfg.Path, cfg.Dir())
	}

	if _, err := Load(filepath.Join(dir, "missing.yaml"), Options{}); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing file: err = %v, want fs.ErrNotExist", err)
	}
}

func TestComponentErrors(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"name with space", "components:\n  my app:\n    env_file: .env\n    vars: {}\n", `invalid component name "my app"`},
		{"name with slash", "components:\n  a/b:\n    env_file: .env\n    vars: {}\n", `invalid component name "a/b"`},
		{"not a mapping", "components:\n  a: x\n", `component "a" must be a mapping`},
		{"components is a list", "components:\n  - a\n", "components must be a mapping"},
		{"empty env_file", "components:\n  a:\n    env_file: \"\"\n    vars: {}\n", "env_file must not be empty"},
		{"vars is a list", "components:\n  a:\n    env_file: .env\n    vars: [A]\n", "vars must be a mapping"},
		{"mapping value", "components:\n  a:\n    env_file: .env\n    vars:\n      A: {b: 1}\n", "A must be a string, number or boolean, not a mapping"},
		{"name is a list", "name: [dev]\ncomponents:\n  a:\n    env_file: .env\n    vars: {}\n", "name must be a string, number or boolean, not a list"},
		{"non-string key", "components:\n  a:\n    env_file: .env\n    vars:\n      ? [A]\n      : x\n", "keys must be strings"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parse(t, tt.src, Options{})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v\nwant it to contain %q", err, tt.want)
			}
		})
	}
}

func TestEmptyVarsAndLookups(t *testing.T) {
	// An absolute env_file is used as-is rather than joined onto the config
	// directory. t.TempDir is absolute on every OS (a path like /abs/x has
	// no drive letter, so it isn't absolute on Windows).
	abs := filepath.Join(t.TempDir(), "elsewhere", ".env")
	cfg := mustParse(t, "components:\n  a:\n    env_file: \""+filepath.ToSlash(abs)+"\"\n    vars:\n")
	c := cfg.Component("a")
	if len(c.Vars) != 0 {
		t.Errorf("null vars should parse as no variables, got %d", len(c.Vars))
	}
	if got := cfg.EnvFilePath(c); got != abs {
		t.Errorf("absolute env_file resolved to %q", got)
	}
	if cfg.Component("nope") != nil || c.Var("NOPE") != nil {
		t.Error("lookups of unknown names should return nil")
	}
}

func TestDiagnosticPathOutsideWorkingDir(t *testing.T) {
	// Files outside the working directory are reported by absolute path.
	path := filepath.Join(t.TempDir(), "envhaul.yaml")
	_, err := Parse(path, []byte("components: {}\n"), Options{})
	if err == nil || !strings.Contains(err.Error(), filepath.ToSlash(path)) && !strings.Contains(err.Error(), path) {
		t.Errorf("error = %v, want it to name %s", err, path)
	}
}

func TestSourceBlocks(t *testing.T) {
	cfg := mustParse(t, `
vault:
  address: https://vault.example.com
  mount: kv
  profiles:
    prod:
      address: https://vault.prod.example.com
      token_env: VAULT_PROD_TOKEN
    empty:
onepassword:
  account: team.1password.com
gcp:
components:
  a:
    env_file: .env
    vars: {}
`)
	v := cfg.Source("vault")
	if got := v.Setting("", "address"); got != "https://vault.example.com" {
		t.Errorf("default address = %q", got)
	}
	if got := v.Setting("prod", "address"); got != "https://vault.prod.example.com" {
		t.Errorf("profile address = %q", got)
	}
	if got := v.Setting("prod", "mount"); got != "kv" {
		t.Errorf("profile should inherit mount, got %q", got)
	}
	if got := v.Setting("missing", "mount"); got != "kv" {
		t.Errorf("unknown profile should fall back to defaults, got %q", got)
	}
	if !v.HasProfile("prod") || !v.HasProfile("empty") || v.HasProfile("missing") {
		t.Error("HasProfile mismatch")
	}
	if cfg.Source("onepassword").Setting("", "account") != "team.1password.com" {
		t.Error("onepassword account not parsed")
	}
	if cfg.Source("gcp") == nil {
		t.Error("an empty gcp block should still be present")
	}
	var nilSource *Source
	if cfg.Source("azure") != nil || nilSource.Setting("x", "y") != "" || nilSource.HasProfile("x") {
		t.Error("absent blocks should be nil and safe to query")
	}
}

func TestSourceBlockErrors(t *testing.T) {
	tail := "components:\n  a:\n    env_file: .env\n    vars: {}\n"
	tests := []struct{ name, src, want string }{
		{"unknown setting", "vault:\n  url: x\n", `unknown key "url" (allowed: address, kv_version, mount, namespace, profiles, token_env)`},
		{"unknown profile setting", "gcp:\n  profiles:\n    p:\n      region: x\n", `unknown key "region"`},
		{"not a mapping", "azure: x\n", "azure must be a mapping of settings"},
		{"profiles not a mapping", "vault:\n  profiles: [a]\n", "vault.profiles must be a mapping"},
		{"profile not a mapping", "vault:\n  profiles:\n    p: x\n", `vault profile "p" must be a mapping`},
		{"bad profile name", "vault:\n  profiles:\n    \"a b\":\n      mount: x\n", `invalid vault profile name "a b"`},
		{"bad kv version", "vault:\n  kv_version: 3\n", "vault.kv_version must be 1 or 2"},
		{"setting is a list", "onepassword:\n  account: [a]\n", "onepassword.account must be a string"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parse(t, tt.src+tail, Options{})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v\nwant it to contain %q", err, tt.want)
			}
		})
	}
}

func TestValidateRefSeesSettingsBelowComponents(t *testing.T) {
	// The azure block comes after the component that depends on it.
	src := "components:\n  a:\n    env_file: .env\n    vars:\n      A: \"{{azkv:db}}\"\nazure:\n  vault: dev-kv\n"
	var seen string
	_, err := parse(t, src, Options{ValidateRef: func(cfg *Config, r *template.Ref) error {
		seen = cfg.Source("azure").Setting(r.Profile, "vault")
		return nil
	}})
	if err != nil || seen != "dev-kv" {
		t.Errorf("err=%v, validator saw vault %q; want dev-kv", err, seen)
	}
}

func TestTopLevelSettingsAndNullProfiles(t *testing.T) {
	cfg := mustParse(t, `
version: 1
secret_prefix: myapp/dev/
vault:
  profiles:
onepassword:
  profiles:
    personal:
components:
  a:
    env_file: .env
    vars: {}
`)
	if cfg.Version != 1 || cfg.SecretPrefix != "myapp/dev/" {
		t.Errorf("Version=%d SecretPrefix=%q", cfg.Version, cfg.SecretPrefix)
	}
	if v := cfg.Source("vault"); v == nil || len(v.Profiles) != 0 {
		t.Errorf("null profiles should parse as none: %+v", v)
	}
	if !cfg.Source("onepassword").HasProfile("personal") {
		t.Error("a profile with no settings should still exist")
	}
	var none *Config
	if none.Source("vault") != nil {
		t.Error("Source on a nil Config should be nil")
	}
}

func TestYAMLAliasesAndEmptyDocuments(t *testing.T) {
	cfg := mustParse(t, `
components:
  a:
    env_file: .env
    vars:
      FIRST: &shared "{{ssm:/shared/key}}"
      SECOND: *shared
`)
	if got := cfg.Component("a").Var("SECOND").Raw; got != "{{ssm:/shared/key}}" {
		t.Errorf("alias value = %q", got)
	}
	for _, src := range []string{"# just a comment\n", "---\n"} {
		if _, err := parse(t, src, Options{}); err == nil || !strings.Contains(err.Error(), "empty") {
			t.Errorf("%q: err = %v, want an empty-file error", src, err)
		}
	}
}

func TestDiagnosticsSortedByPosition(t *testing.T) {
	// Provider validation runs after parsing; its findings are merged with
	// the parser's in file order.
	src := "components:\n  a:\n    env_file: .env\n    vars:\n      A: \"{{x:1}}\"\n      1B: x\n      C: \"{{x:2}}\"\n"
	_, err := parse(t, src, Options{ValidateRef: func(_ *Config, r *template.Ref) error {
		return errors.New("bad " + r.Path)
	}})
	var diags Diagnostics
	if !errors.As(err, &diags) || len(diags) != 3 {
		t.Fatalf("err = %v", err)
	}
	for i := 1; i < len(diags); i++ {
		if diags[i-1].Pos.Line > diags[i].Pos.Line {
			t.Errorf("diagnostics out of order:\n%v", err)
		}
	}
	if !strings.Contains(diags[1].Msg, "invalid variable name") {
		t.Errorf("middle diagnostic = %q, want the parser's line-6 error", diags[1].Msg)
	}
}
