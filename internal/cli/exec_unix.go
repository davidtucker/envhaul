//go:build !windows

package cli

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// execCommand replaces the current process so signals and exit codes go
// straight to the command. When output is redirected to something other
// than the process's own stdio (tests), it runs the command as a child.
func execCommand(a *app, path string, argv, env []string) error {
	if a.opts.Stdout == os.Stdout && a.opts.Stderr == os.Stderr && a.opts.Stdin == os.Stdin {
		return syscall.Exec(path, argv, env)
	}
	return runChild(a, path, argv, env)
}

func runChild(a *app, path string, argv, env []string) error {
	cmd := exec.Command(path, argv[1:]...)
	cmd.Env = env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = a.opts.Stdin, a.opts.Stdout, a.opts.Stderr
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return &ExitError{Code: exit.ExitCode()}
		}
		return err
	}
	return nil
}
