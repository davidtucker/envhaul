// Package gitcheck reports whether generated files are ignored by git, so
// envhaul can warn before secrets end up in a commit.
package gitcheck

import (
	"errors"
	"os/exec"
	"path/filepath"
)

// Status is the result of checking one path.
type Status int

const (
	// Unknown means git is unavailable or the path is not in a repository.
	Unknown Status = iota
	Ignored
	NotIgnored
)

// Check runs `git check-ignore` for path.
func Check(path string) Status {
	git, err := exec.LookPath("git")
	if err != nil {
		return Unknown
	}
	cmd := exec.Command(git, "check-ignore", "-q", "--", filepath.Base(path))
	cmd.Dir = filepath.Dir(path)
	err = cmd.Run()
	if err == nil {
		return Ignored
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return NotIgnored
	}
	// 128: not a git repository (or the directory does not exist yet).
	return Unknown
}
