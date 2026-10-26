package cli

import (
	"bytes"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/davidtucker/envhaul/internal/config"
)

// run executes the CLI in-process with captured output.
func run(t *testing.T, terminal bool, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = Run(args, Options{
		Stdin:       strings.NewReader(""),
		Stdout:      &out,
		Stderr:      &errOut,
		NewRegistry: fakeRegistry,
		IsTerminal:  func(any) bool { return terminal },
	})
	return out.String(), errOut.String(), code
}

// inProject switches to a temporary directory containing files.
func inProject(t *testing.T, files map[string]string) {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(dir)
	t.Setenv("ENVHAUL_CONFIG", "")
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")
}

const simpleConfig = "components:\n  app:\n    env_file: .env\n    vars:\n      KEY: \"{{ssm:/key}}\"\n"

func TestColorOnlyOnTerminals(t *testing.T) {
	inProject(t, map[string]string{"envhaul.yaml": simpleConfig})

	out, _, code := run(t, true, "validate")
	if code != 0 || !strings.Contains(out, "\x1b[32mok:\x1b[0m") {
		t.Errorf("terminal output should be colored: code=%d %q", code, out)
	}

	out, _, _ = run(t, false, "validate")
	if strings.Contains(out, "\x1b[") {
		t.Errorf("non-terminal output must not contain escape codes: %q", out)
	}

	out, _, _ = run(t, true, "--no-color", "validate")
	if strings.Contains(out, "\x1b[") {
		t.Errorf("--no-color must disable color: %q", out)
	}

	t.Setenv("NO_COLOR", "1")
	out, _, _ = run(t, true, "validate")
	if strings.Contains(out, "\x1b[") {
		t.Errorf("NO_COLOR must disable color: %q", out)
	}

	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "dumb")
	out, _, _ = run(t, true, "validate")
	if strings.Contains(out, "\x1b[") {
		t.Errorf("TERM=dumb must disable color: %q", out)
	}
}

func TestErrorsAreColoredOnStderr(t *testing.T) {
	inProject(t, nil)
	_, errOut, code := run(t, true, "-c", "missing.yaml", "list")
	if code != 1 || !strings.Contains(errOut, "\x1b[1;31merror:\x1b[0m") {
		t.Errorf("code=%d stderr=%q, want a red error", code, errOut)
	}
}

func TestBareCommandShowsBannerOnTerminal(t *testing.T) {
	inProject(t, nil)
	out, _, code := run(t, true)
	if code != 0 || !strings.Contains(out, "envhaul") || !strings.Contains(out, "Usage:") {
		t.Fatalf("code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "\x1b[") {
		t.Error("terminal banner should be colored")
	}

	out, _, _ = run(t, false)
	if strings.Contains(out, "███") || strings.Contains(out, "___") {
		t.Error("the banner should only be printed to a terminal")
	}
}

func TestExecRunsChildWithRedirectedOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	inProject(t, map[string]string{
		"envhaul.yaml":  simpleConfig,
		"fake-aws.json": `{"ssm": {"default|/key": "from-config"}}`,
	})
	out, _, code := run(t, false, "exec", "app", "--", "sh", "-c", `echo "$KEY"; exit 3`)
	if strings.TrimSpace(out) != "from-config" {
		t.Errorf("child stdout = %q", out)
	}
	if code != 3 {
		t.Errorf("exit code = %d, want the child's 3", code)
	}

	_, errOut, code := run(t, false, "exec", "app", "--", "definitely-not-a-command-envhaul")
	if code != 1 || !strings.Contains(errOut, "executable file not found") {
		t.Errorf("missing command: code=%d stderr=%q", code, errOut)
	}
}

func TestIsTerminal(t *testing.T) {
	if isTerminal(&bytes.Buffer{}) {
		t.Error("a buffer is not a terminal")
	}
	f, err := os.CreateTemp(t.TempDir(), "x")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if isTerminal(f) {
		t.Error("a regular file is not a terminal")
	}
	_ = f.Close()
	if isTerminal(f) {
		t.Error("a closed file is not a terminal")
	}
}

func TestExitErrorAndDefaultRegistry(t *testing.T) {
	if got := (&ExitError{Code: 3}).Error(); got != "exit status 3" {
		t.Errorf("ExitError.Error() = %q", got)
	}
	reg := defaultRegistry(&config.Config{}, &Globals{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if got := strings.Join(reg.Schemes(), ","); got != "azkv,gcp,op,secret,ssm,vault" {
		t.Errorf("default schemes = %s", got)
	}
}
