//go:build integration

// Package integration runs envhaul against an AWS emulator (moto). Start it with
// `make emulator-up` and run `make test-integration`.
package integration

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
)

func endpoint() string {
	if e := os.Getenv("AWS_EMULATOR_ENDPOINT"); e != "" {
		return e
	}
	return "http://127.0.0.1:5000"
}

// awsConfigFile writes a shared config with two profiles pointing at
// the emulator, standing in for two AWS accounts.
func awsConfigFile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config")
	creds := filepath.Join(dir, "credentials")
	var b strings.Builder
	for _, p := range []string{"it-dev", "it-shared"} {
		b.WriteString("[profile " + p + "]\nregion = us-east-1\nendpoint_url = " + endpoint() + "\n\n")
	}
	if err := os.WriteFile(cfg, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	c := "[it-dev]\naws_access_key_id = test\naws_secret_access_key = test\n\n[it-shared]\naws_access_key_id = test\naws_secret_access_key = test\n"
	if err := os.WriteFile(creds, []byte(c), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AWS_CONFIG_FILE", cfg)
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", creds)
	t.Setenv("AWS_PROFILE", "")
	return cfg
}

func seed(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion("us-east-1"),
		config.WithBaseEndpoint(endpoint()),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test", "test", "")),
	)
	if err != nil {
		t.Fatal(err)
	}
	ssmc := ssm.NewFromConfig(cfg)
	params := map[string]struct {
		v string
		t ssmtypes.ParameterType
	}{
		"/it/dev/api/PLAIN":  {"plain-value", ssmtypes.ParameterTypeString},
		"/it/dev/api/SECURE": {"s3cr3t value", ssmtypes.ParameterTypeSecureString},
		"/it/dev/api/LIST":   {"a,b,c", ssmtypes.ParameterTypeStringList},
	}
	for i := range 12 {
		params["/it/dev/bulk/P"+string(rune('a'+i))] = struct {
			v string
			t ssmtypes.ParameterType
		}{"bulk", ssmtypes.ParameterTypeString}
	}
	for name, p := range params {
		if _, err := ssmc.PutParameter(ctx, &ssm.PutParameterInput{
			Name: aws.String(name), Value: aws.String(p.v), Type: p.t, Overwrite: aws.Bool(true),
		}); err != nil {
			t.Fatalf("seeding %s: %v (is the emulator running at %s?)", name, err, endpoint())
		}
	}
	sm := secretsmanager.NewFromConfig(cfg)
	secrets := map[string]string{
		"it/db":    `{"user":"admin","pass":"p@ss/w:rd"}`,
		"it/plain": "whole-secret",
	}
	for id, v := range secrets {
		_, err := sm.CreateSecret(ctx, &secretsmanager.CreateSecretInput{Name: aws.String(id), SecretString: aws.String(v)})
		if err != nil && strings.Contains(err.Error(), "ResourceExists") {
			_, err = sm.PutSecretValue(ctx, &secretsmanager.PutSecretValueInput{SecretId: aws.String(id), SecretString: aws.String(v)})
		}
		if err != nil {
			t.Fatalf("seeding secret %s: %v", id, err)
		}
	}
}

func build(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "envhaul")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", bin, "../../cmd/envhaul")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	return bin
}

const configYAML = `name: it
aws_profile: it-dev
ssm_prefix: /it/dev
components:
  api:
    env_file: api/.env
    vars:
      PLAIN: "{{ssm:api/PLAIN}}"
      SECURE: "{{ssm:api/SECURE}}"
      LIST: "{{ssm:api/LIST}}"
      DB_URL: "postgresql://{{secret:it/db#user@it-shared}}:{{secret:it/db#pass@it-shared|urlencode}}@localhost/app"
      WHOLE: "{{secret:it/plain}}"
      MISSING: "{{ssm:api/DOES_NOT_EXIST}}"
      DEFAULTED: "{{ssm:api/DOES_NOT_EXIST|default:fallback}}"
  bulk:
    env_file: bulk/.env
    vars:
      A: "{{ssm:bulk/Pa}}"
      B: "{{ssm:bulk/Pb}}"
      C: "{{ssm:bulk/Pc}}"
      D: "{{ssm:bulk/Pd}}"
      E: "{{ssm:bulk/Pe}}"
      F: "{{ssm:bulk/Pf}}"
      G: "{{ssm:bulk/Pg}}"
      H: "{{ssm:bulk/Ph}}"
      I: "{{ssm:bulk/Pi}}"
      J: "{{ssm:bulk/Pj}}"
      K: "{{ssm:bulk/Pk}}"
      L: "{{ssm:bulk/Pl}}"
`

func TestSyncAgainstEmulator(t *testing.T) {
	awsConfigFile(t)
	seed(t)
	bin := build(t)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "envhaul.yaml"), []byte(configYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) (string, string, error) {
		cmd := exec.Command(bin, args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "NO_COLOR=1")
		var stdout, stderr strings.Builder
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		return stdout.String(), stderr.String(), err
	}

	stdout, stderr, err := run("sync", "-v")
	if err != nil {
		t.Fatalf("sync failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if !strings.Contains(stderr, "api.MISSING: ssm:/it/dev/api/DOES_NOT_EXIST@it-dev not found") {
		t.Errorf("missing-value warning not printed:\n%s", stderr)
	}
	for _, secret := range []string{"s3cr3t", "p@ss", "admin", "whole-secret"} {
		if strings.Contains(stderr, secret) {
			t.Errorf("verbose log leaked secret value %q:\n%s", secret, stderr)
		}
	}

	api, err := os.ReadFile(filepath.Join(dir, "api", ".env"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"PLAIN=plain-value\n",
		"SECURE='s3cr3t value'\n",
		"LIST=a,b,c\n",
		"DB_URL=postgresql://admin:p%40ss%2Fw%3Ard@localhost/app\n",
		"WHOLE=whole-secret\n",
		"MISSING=\n",
		"DEFAULTED=fallback\n",
	} {
		if !strings.Contains(string(api), want) {
			t.Errorf("api/.env missing %q:\n%s", want, api)
		}
	}
	bulk, err := os.ReadFile(filepath.Join(dir, "bulk", ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(bulk), "=bulk\n"); n != 12 {
		t.Errorf("bulk/.env has %d values, want 12 (batching across GetParameters calls):\n%s", n, bulk)
	}

	if _, _, err := run("diff", "--exit-code"); err != nil {
		t.Errorf("diff after sync should report no changes: %v", err)
	}
	if _, stderr, err := run("sync", "--strict"); err == nil || !strings.Contains(stderr, "strict mode") {
		t.Errorf("--strict should fail on the missing parameter: err=%v\n%s", err, stderr)
	}
	out, _, err := run("get", "api", "DB_URL")
	if err != nil || strings.TrimSpace(out) != "postgresql://admin:p%40ss%2Fw%3Ard@localhost/app" {
		t.Errorf("get = %q, %v", out, err)
	}
}
