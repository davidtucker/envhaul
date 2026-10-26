//go:build windows

package cli

import (
	"errors"
	"os/exec"
)

// execCommand runs the command as a child (Windows has no exec) and exits
// with its status.
func execCommand(a *app, path string, argv, env []string) error {
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
