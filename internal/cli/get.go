package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/davidtucker/envhaul/internal/config"
	"github.com/davidtucker/envhaul/internal/resolve"
)

func (a *app) getCmd() *cobra.Command {
	var noNewline bool
	cmd := &cobra.Command{
		Use:   "get <component> <VAR>",
		Short: "Print one resolved value",
		Long: `Resolve a single variable and print its value to stdout. Only the references
used by that variable are fetched. Unlike sync, a missing value is an error
(unless the reference has a |default).`,
		Example: `  envhaul get api OPENAI_API_KEY
  export DATABASE_URL="$(envhaul get api DATABASE_URL)"`,
		Args: cobra.ExactArgs(2),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			if len(args) == 0 {
				return a.completeComponents(cmd, nil, toComplete)
			}
			cfg, err := a.loadConfig()
			if err != nil || len(args) > 1 {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			comp := cfg.Component(args[0])
			if comp == nil {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			var names []string
			for _, v := range comp.Vars {
				names = append(names, v.Name)
			}
			return names, cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := a.loadConfig()
			if err != nil {
				return err
			}
			comp := cfg.Component(args[0])
			if comp == nil {
				_, err := cfg.Select(args[:1])
				return err
			}
			v := comp.Var(args[1])
			if v == nil {
				var names []string
				for _, cv := range comp.Vars {
					names = append(names, cv.Name)
				}
				return fmt.Errorf("component %q has no variable %q (available: %s)", comp.Name, args[1], strings.Join(names, ", "))
			}
			single := &config.Component{Name: comp.Name, EnvFile: comp.EnvFile, Vars: []*config.Var{v}}
			r := &resolve.Resolver{Registry: a.registry(cfg), Strict: true}
			resolved, _, err := r.Resolve(cmd.Context(), []*config.Component{single})
			if err != nil {
				return err
			}
			fmt.Fprint(a.opts.Stdout, resolved[0].Values[0].Value)
			if !noNewline {
				fmt.Fprintln(a.opts.Stdout)
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&noNewline, "no-newline", "n", false, "do not print a trailing newline")
	return cmd
}
