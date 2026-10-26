package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

func (a *app) execCmd() *cobra.Command {
	var strict bool
	cmd := &cobra.Command{
		Use:   "exec <component> -- <command> [args...]",
		Short: "Run a command with a component's variables, without writing a file",
		Long: `Resolve a component and run a command with its variables added to the current
environment. Component values override variables already set. Nothing is
written to disk, so secrets never touch the filesystem.

On Unix the command replaces the envhaul process; on Windows envhaul waits for
it and exits with its status.`,
		Example: `  envhaul exec api -- npm start
  envhaul exec infrastructure -- cdk deploy`,
		Args: func(cmd *cobra.Command, args []string) error {
			dash := cmd.ArgsLenAtDash()
			if dash != 1 || len(args) < 2 {
				return fmt.Errorf("usage: envhaul exec <component> -- <command> [args...]")
			}
			return nil
		},
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			if len(args) == 0 {
				return a.completeComponents(cmd, args, toComplete)
			}
			return nil, cobra.ShellCompDirectiveDefault
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			_, _, resolved, err := a.resolve(cmd.Context(), args[:1], strict)
			if err != nil {
				return err
			}
			env := os.Environ()
			for _, v := range resolved[0].Values {
				env = setEnv(env, v.Name, v.Value)
			}
			path, err := exec.LookPath(args[1])
			if err != nil {
				return err
			}
			a.logger.Debug("exec", "command", path, "vars", len(resolved[0].Values))
			return execCommand(a, path, args[1:], env)
		},
	}
	cmd.Flags().BoolVar(&strict, "strict", false, "fail if any referenced value does not exist")
	return cmd
}

// setEnv sets key in env, replacing any existing entry.
func setEnv(env []string, key, value string) []string {
	prefix := key + "="
	out := env[:0:0]
	for _, kv := range env {
		if !strings.HasPrefix(kv, prefix) {
			out = append(out, kv)
		}
	}
	return append(out, prefix+value)
}
