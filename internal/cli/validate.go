package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/davidtucker/envhaul/internal/config"
	"github.com/davidtucker/envhaul/internal/gitcheck"
)

func (a *app) validateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Check the config file for errors (offline)",
		Long: `Check the config file without contacting AWS: YAML syntax, unknown keys,
variable names, reference syntax, sources and pipe functions, and duplicate
env files. Every problem is reported with its file:line:column. Also warns
about env files that git would not ignore.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := a.loadConfig()
			if err != nil {
				var diags config.Diagnostics
				if errors.As(err, &diags) {
					for _, d := range diags {
						fmt.Fprintln(a.opts.Stderr, d.String())
					}
					fmt.Fprintf(a.opts.Stderr, "%s %s\n", a.paint(a.opts.Stderr, "1;31", "invalid:"), plural(len(diags), "problem"))
					return &ExitError{Code: 1}
				}
				return err
			}
			vars, refs := 0, 0
			for _, c := range cfg.Components {
				vars += len(c.Vars)
				for _, v := range c.Vars {
					refs += len(v.Template.Refs())
				}
				if gitcheck.Check(cfg.EnvFilePath(c)) == gitcheck.NotIgnored {
					a.warnf("%s (component %s) is not ignored by git", c.EnvFile, c.Name)
				}
			}
			fmt.Fprintf(a.opts.Stdout, "%s %s is valid: %s, %s, %s\n",
				a.paint(a.opts.Stdout, "32", "ok:"), relPath(cfg.Path),
				plural(len(cfg.Components), "component"), plural(vars, "variable"), plural(refs, "reference"))
			return nil
		},
	}
}
