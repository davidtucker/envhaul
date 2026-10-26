package gitcheck

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCheck(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	if Check(filepath.Join(dir, ".env")) != Unknown {
		t.Error("outside a repository the status should be Unknown")
	}
	if out, err := exec.Command("git", "init", "-q", dir).CombinedOutput(); err != nil {
		t.Skipf("git init failed: %v %s", err, out)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(".env\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := Check(filepath.Join(dir, ".env")); got != Ignored {
		t.Errorf(".env status = %v, want Ignored", got)
	}
	if got := Check(filepath.Join(dir, "other.env")); got != NotIgnored {
		t.Errorf("other.env status = %v, want NotIgnored", got)
	}
}
