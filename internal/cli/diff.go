package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/spf13/cobra"

	"github.com/davidtucker/envhaul/internal/dotenv"
)

func (a *app) diffCmd() *cobra.Command {
	var showValues, exitCode, strict bool
	cmd := &cobra.Command{
		Use:   "diff [component...]",
		Short: "Show what sync would change, without writing",
		Long: `Resolve every variable and compare the result with the env files on disk.

Values are masked unless --show-values is given. With --exit-code, diff exits
with status 1 when any file would change, which is useful in CI or git hooks.`,
		Example: `  envhaul diff
  envhaul diff api --show-values
  envhaul diff --exit-code`,
		ValidArgsFunction: a.completeComponents,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, comps, resolved, err := a.resolve(cmd.Context(), args, strict)
			if err != nil {
				return err
			}
			w := a.opts.Stdout
			changedFiles := 0
			for i, rc := range resolved {
				path := cfg.EnvFilePath(comps[i])
				want := render(cfg, rc)
				name := relPath(path)

				current, err := os.ReadFile(path)
				if errors.Is(err, fs.ErrNotExist) {
					changedFiles++
					fmt.Fprintf(w, "%s %s\n", a.paint(w, "1", name), a.paint(w, "32", "(new file)"))
					for _, v := range rc.Values {
						fmt.Fprintf(w, "  %s %s\n", a.paint(w, "32", "+"), a.show(v.Name, "", v.Value, showValues, true))
					}
					continue
				}
				if err != nil {
					return err
				}
				if bytes.Equal(current, want) {
					fmt.Fprintf(w, "%s %s\n", a.paint(w, "1", name), a.paint(w, "2", "(up to date)"))
					continue
				}
				changedFiles++
				fmt.Fprintf(w, "%s\n", a.paint(w, "1", name))

				old, err := dotenv.Parse(current)
				if err != nil {
					fmt.Fprintf(w, "  %s existing file could not be parsed (%v); sync will replace it\n", a.paint(w, "33", "~"), err)
					continue
				}
				oldVals := map[string]string{}
				for _, p := range old {
					oldVals[p.Key] = p.Value
				}
				newKeys := map[string]bool{}
				lines := 0
				for _, v := range rc.Values {
					newKeys[v.Name] = true
					prev, existed := oldVals[v.Name]
					switch {
					case !existed:
						fmt.Fprintf(w, "  %s %s\n", a.paint(w, "32", "+"), a.show(v.Name, "", v.Value, showValues, true))
						lines++
					case prev != v.Value:
						fmt.Fprintf(w, "  %s %s\n", a.paint(w, "33", "~"), a.show(v.Name, prev, v.Value, showValues, false))
						lines++
					}
				}
				for _, p := range old {
					if !newKeys[p.Key] {
						fmt.Fprintf(w, "  %s %s\n", a.paint(w, "31", "-"), p.Key)
						lines++
					}
				}
				if lines == 0 {
					fmt.Fprintf(w, "  %s formatting, order or header only\n", a.paint(w, "33", "~"))
				}
			}
			if exitCode && changedFiles > 0 {
				return &ExitError{Code: 1}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&showValues, "show-values", false, "print values instead of masking them")
	cmd.Flags().BoolVar(&exitCode, "exit-code", false, "exit with status 1 if any file would change")
	cmd.Flags().BoolVar(&strict, "strict", false, "fail if any referenced value does not exist")
	return cmd
}

func (a *app) show(name, old, value string, showValues, added bool) string {
	if !showValues {
		return name
	}
	if added {
		return name + "=" + dotenv.Quote(value)
	}
	return fmt.Sprintf("%s: %s → %s", name, dotenv.Quote(old), dotenv.Quote(value))
}
