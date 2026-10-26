package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"

	"github.com/davidtucker/envhaul/internal/brand"
	"github.com/davidtucker/envhaul/internal/version"
)

func (a *app) versionCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			info := version.Get()
			out := a.opts.Stdout
			if asJSON {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(info)
			}
			if a.opts.IsTerminal(out) {
				fmt.Fprint(out, brand.Small(a.style(out)))
				fmt.Fprintln(out)
			}
			fmt.Fprintf(out, "envhaul %s\n", info.Version)
			fmt.Fprintf(out, "  commit:   %s\n", info.Commit)
			fmt.Fprintf(out, "  built:    %s\n", info.Date)
			fmt.Fprintf(out, "  go:       %s\n", info.GoVersion)
			fmt.Fprintf(out, "  platform: %s\n", info.Platform)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	return cmd
}

// genCmd holds hidden generators used by the release process.
func (a *app) genCmd() *cobra.Command {
	gen := &cobra.Command{
		Use:    "gen",
		Short:  "Generate documentation (used by the release process)",
		Hidden: true,
	}
	gen.AddCommand(
		&cobra.Command{
			Use:   "man <dir>",
			Short: "Generate man pages",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				if err := os.MkdirAll(args[0], 0o755); err != nil {
					return err
				}
				root := cmd.Root()
				root.DisableAutoGenTag = true
				return doc.GenManTree(root, &doc.GenManHeader{Title: "ENVHAUL", Section: "1", Source: "envhaul " + version.Get().Version}, args[0])
			},
		},
		&cobra.Command{
			Use:   "docs <dir>",
			Short: "Generate Markdown command reference",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				if err := os.MkdirAll(args[0], 0o755); err != nil {
					return err
				}
				root := cmd.Root()
				root.DisableAutoGenTag = true
				return doc.GenMarkdownTreeCustom(root, args[0],
					func(string) string { return "" },
					filepath.Base)
			},
		},
	)
	return gen
}
