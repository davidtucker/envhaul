package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/davidtucker/envhaul/internal/config"
	"github.com/davidtucker/envhaul/internal/provider"
	"github.com/davidtucker/envhaul/internal/providers"
)

// listEntry is the stable --json output of `envhaul list`.
type listEntry struct {
	Component string       `json:"component"`
	EnvFile   string       `json:"env_file"`
	Var       string       `json:"var"`
	Kind      string       `json:"kind"` // inline | reference | template
	Sources   []listSource `json:"sources,omitempty"`
}

type listSource struct {
	Scheme  string   `json:"scheme"`
	Path    string   `json:"path"`
	Key     string   `json:"key,omitempty"`
	Profile string   `json:"profile,omitempty"`
	Region  string   `json:"region,omitempty"`
	Pipes   []string `json:"pipes,omitempty"`
}

func (a *app) listCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list [component...]",
		Short: "List variables and where their values come from (offline)",
		Long: `List every variable and its source: inline, a single reference, or a template
mixing text and references. Prefixes and default profiles are applied so the
output shows exactly what would be fetched. No AWS calls are made.`,
		Example: `  envhaul list
  envhaul list api --json | jq '.[] | select(.kind != "inline")'`,
		ValidArgsFunction: a.completeComponents,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := a.loadConfig()
			if err != nil {
				return err
			}
			comps, err := cfg.Select(args)
			if err != nil {
				return err
			}
			// Offline registry: same prefix and profile rules, no clients.
			reg := providers.New(providers.Options{
				Config: cfg, ProfileOverride: a.globals.Profile, ProfileOverrideSet: a.globals.ProfileSet, RegionOverride: a.globals.Region,
			})
			entries := listEntries(comps, reg)

			if asJSON {
				enc := json.NewEncoder(a.opts.Stdout)
				enc.SetIndent("", "  ")
				if entries == nil {
					entries = []listEntry{}
				}
				return enc.Encode(entries)
			}
			tw := tabwriter.NewWriter(a.opts.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "COMPONENT\tVAR\tSOURCE")
			for _, e := range entries {
				fmt.Fprintf(tw, "%s\t%s\t%s\n", e.Component, e.Var, describeEntry(e))
			}
			return tw.Flush()
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "output JSON")
	return cmd
}

func listEntries(comps []*config.Component, reg *provider.Registry) []listEntry {
	var out []listEntry
	for _, c := range comps {
		for _, v := range c.Vars {
			e := listEntry{Component: c.Name, EnvFile: c.EnvFile, Var: v.Name, Kind: "inline"}
			refs := v.Template.Refs()
			if len(refs) > 0 {
				e.Kind = "template"
				if len(v.Template.Parts) == 1 {
					e.Kind = "reference"
				}
			}
			for _, r := range refs {
				p, _ := reg.Get(r.Scheme)
				req := p.Request(r)
				src := listSource{Scheme: r.Scheme, Path: req.Path, Key: r.Key, Profile: req.Profile, Region: req.Region}
				for _, pc := range r.Pipes {
					src.Pipes = append(src.Pipes, pc.String())
				}
				e.Sources = append(e.Sources, src)
			}
			out = append(out, e)
		}
	}
	return out
}

func describeEntry(e listEntry) string {
	if e.Kind == "inline" {
		return "inline"
	}
	parts := make([]string, len(e.Sources))
	for i, s := range e.Sources {
		d := provider.Describe(s.Scheme, provider.Request{Path: s.Path, Profile: s.Profile}, s.Key)
		for _, p := range s.Pipes {
			d += "|" + p
		}
		parts[i] = d
	}
	if e.Kind == "template" {
		return "template: " + strings.Join(parts, ", ")
	}
	return parts[0]
}
