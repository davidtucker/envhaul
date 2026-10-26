//go:build integration

package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// vaultCall makes an authenticated Vault/OpenBao API call; namespace may be
// empty.
func vaultCall(t *testing.T, addr, namespace, method, path string, body any) (int, string) {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, addr+"/v1/"+path, r)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Vault-Token", "root")
	if namespace != "" {
		req.Header.Set("X-Vault-Namespace", namespace)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v (is the backend running at %s? run `make emulator-up`)", method, path, err, addr)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(out)
}

func mustCall(t *testing.T, addr, namespace, method, path string, body any) {
	t.Helper()
	status, out := vaultCall(t, addr, namespace, method, path, body)
	if status >= 300 && !strings.Contains(out, "already in use") && !strings.Contains(out, "already exists") {
		t.Fatalf("%s %s: HTTP %d %s", method, path, status, out)
	}
}

func TestVaultAndOpenBao(t *testing.T) {
	vaultAddr := envOr("VAULT_TEST_ADDR", "http://127.0.0.1:8200")
	baoAddr := envOr("OPENBAO_TEST_ADDR", "http://127.0.0.1:8210")

	// Vault: a KV v2 secret with two versions, and a KV v1 mount.
	mustCall(t, vaultAddr, "", "POST", "secret/data/app/db", map[string]any{"data": map[string]string{"user": "admin", "password": "first"}})
	mustCall(t, vaultAddr, "", "POST", "secret/data/app/db", map[string]any{"data": map[string]string{"user": "admin", "password": "p@ss w/rd"}})
	mustCall(t, vaultAddr, "", "POST", "sys/mounts/legacy", map[string]any{"type": "kv", "options": map[string]string{"version": "1"}})
	mustCall(t, vaultAddr, "", "POST", "legacy/svc", map[string]string{"token": "v1-token"})

	// OpenBao: a secret at the root, and one inside the "team" namespace.
	mustCall(t, baoAddr, "", "POST", "secret/data/app/db", map[string]any{"data": map[string]string{"password": "bao-pass"}})
	mustCall(t, baoAddr, "", "POST", "sys/namespaces/team", map[string]any{})
	mustCall(t, baoAddr, "team", "POST", "sys/mounts/secret", map[string]any{"type": "kv", "options": map[string]string{"version": "2"}})
	mustCall(t, baoAddr, "team", "POST", "secret/data/app/db", map[string]any{"data": map[string]string{"password": "team-pass"}})

	dir := t.TempDir()
	cfg := fmt.Sprintf(`vault:
  address: %s
  profiles:
    legacy:
      mount: legacy
      kv_version: 1
    bao:
      address: %s
      token_env: BAO_TOKEN
    bao-team:
      address: %s
      namespace: team
      token_env: BAO_TOKEN
components:
  app:
    env_file: .env
    vars:
      DB_USER: "{{vault:app/db#user}}"
      DB_PASS: "{{vault:app/db#password|urlencode}}"
      FIRST_PASS: "{{vault:app/db:1#password}}"
      ABSOLUTE: "{{vault:/secret/app/db#user}}"
      LEGACY: "{{vault:svc#token@legacy}}"
      BAO: "{{vault:app/db#password@bao}}"
      BAO_TEAM: "{{vault:app/db#password@bao-team}}"
      WHOLE: "{{vault:app/db}}"
      MISSING: "{{vault:nope#x|default:fallback}}"
`, vaultAddr, baoAddr, baoAddr)
	if err := os.WriteFile(filepath.Join(dir, "envhaul.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	bin := build(t)
	cmd := exec.Command(bin, "sync", "--strict", "-v")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "NO_COLOR=1", "VAULT_TOKEN=root", "BAO_TOKEN=root", "VAULT_ADDR=", "VAULT_NAMESPACE=")
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("sync: %v\n%s\n%s", err, stdout.String(), stderr.String())
	}
	for _, secret := range []string{"p@ss", "first", "v1-token", "bao-pass", "team-pass"} {
		if strings.Contains(stderr.String(), secret) {
			t.Errorf("verbose log leaked %q:\n%s", secret, stderr.String())
		}
	}

	env, err := os.ReadFile(filepath.Join(dir, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"DB_USER=admin\n",
		"DB_PASS=p%40ss%20w%2Frd\n",
		"FIRST_PASS=first\n",
		"ABSOLUTE=admin\n",
		"LEGACY=v1-token\n",
		"BAO=bao-pass\n",
		"BAO_TEAM=team-pass\n",
		`WHOLE='{"password":"p@ss w/rd","user":"admin"}'` + "\n",
		"MISSING=fallback\n",
	} {
		if !strings.Contains(string(env), want) {
			t.Errorf(".env missing %q:\n%s", want, env)
		}
	}

	// The OpenBao namespace also works as a bare @namespace, without a profile.
	cfg2 := fmt.Sprintf("vault:\n  address: %s\ncomponents:\n  app:\n    env_file: ns.env\n    vars:\n      P: \"{{vault:app/db#password@team}}\"\n", baoAddr)
	if err := os.WriteFile(filepath.Join(dir, "ns.yaml"), []byte(cfg2), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command(bin, "-c", "ns.yaml", "get", "app", "P")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "VAULT_TOKEN=root", "VAULT_ADDR=", "VAULT_NAMESPACE=")
	out, err := cmd.CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "team-pass" {
		t.Errorf("@namespace get = %q, %v", out, err)
	}
}
