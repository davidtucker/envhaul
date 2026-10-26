// Package cli implements the envhaul command-line interface.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/davidtucker/envhaul/internal/awsclient"
	"github.com/davidtucker/envhaul/internal/brand"
	"github.com/davidtucker/envhaul/internal/config"
	"github.com/davidtucker/envhaul/internal/provider"
	"github.com/davidtucker/envhaul/internal/providers"
	"github.com/davidtucker/envhaul/internal/version"
)

// Options let callers (main and tests) control I/O and provider wiring.
type Options struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	// NewRegistry builds the providers used to fetch values. Defaults to the
	// built-in AWS providers.
	NewRegistry func(cfg *config.Config, g *Globals, logger *slog.Logger) *provider.Registry
	// IsTerminal reports whether a stream is an interactive terminal.
	IsTerminal func(any) bool
}

// Globals are the persistent flags shared by every command.
type Globals struct {
	ConfigPath string
	Profile    string
	// ProfileSet is true when --profile was given, even as "".
	ProfileSet bool
	Region     string
	Verbose    bool
	NoColor    bool
}

// ExitError carries a specific exit code without an additional message.
type ExitError struct{ Code int }

func (e *ExitError) Error() string { return fmt.Sprintf("exit status %d", e.Code) }

type app struct {
	opts    Options
	globals Globals
	logger  *slog.Logger
}

// Run executes envhaul with args and returns the process exit code.
func Run(args []string, opts Options) int {
	if opts.Stdin == nil {
		opts.Stdin = os.Stdin
	}
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	if opts.IsTerminal == nil {
		opts.IsTerminal = isTerminal
	}
	if opts.NewRegistry == nil {
		opts.NewRegistry = defaultRegistry
	}

	a := &app{opts: opts}
	root := a.rootCmd()
	root.SetArgs(args)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	err := root.ExecuteContext(ctx)
	if err == nil {
		return 0
	}
	var exit *ExitError
	if errors.As(err, &exit) {
		return exit.Code
	}
	a.errorf("%v", err)
	return 1
}

func defaultRegistry(cfg *config.Config, g *Globals, logger *slog.Logger) *provider.Registry {
	return providers.New(providers.Options{
		Config:             cfg,
		ProfileOverride:    g.Profile,
		ProfileOverrideSet: g.ProfileSet,
		RegionOverride:     g.Region,
		AWS:                awsclient.NewCache(),
		Logger:             logger,
	})
}

func (a *app) rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "envhaul",
		Short: brand.Tagline,
		Long: brand.Tagline + `

envhaul reads a config file (envhaul.yaml) that lists the variables for each
component of your project. Values are written inline or as references such as
{{ssm:api/KEY}} or {{secret:db#password|urlencode}}, resolved using your named
AWS profiles, and written to .env files.

Documentation: https://davidtucker.github.io/envhaul`,
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version.Get().Version,
		Args:          cobra.NoArgs,
		PersistentPreRun: func(cmd *cobra.Command, _ []string) {
			a.globals.ProfileSet = cmd.Flags().Changed("profile") && cmd.Name() != "init"
			level := slog.LevelWarn
			if a.globals.Verbose {
				level = slog.LevelDebug
			}
			a.logger = slog.New(slog.NewTextHandler(a.opts.Stderr, &slog.HandlerOptions{Level: level}))
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			if a.opts.IsTerminal(a.opts.Stdout) {
				fmt.Fprintln(a.opts.Stdout)
				fmt.Fprint(a.opts.Stdout, brand.Large(a.style(a.opts.Stdout)))
				fmt.Fprintln(a.opts.Stdout)
			}
			return cmd.Help()
		},
	}
	root.SetIn(a.opts.Stdin)
	root.SetOut(a.opts.Stdout)
	root.SetErr(a.opts.Stderr)
	root.SetVersionTemplate("{{.Name}} {{.Version}}\n")

	f := root.PersistentFlags()
	f.StringVarP(&a.globals.ConfigPath, "config", "c", "", "config file (default: $ENVHAUL_CONFIG, or envhaul.yaml found by searching up to the repository root)")
	f.StringVar(&a.globals.Profile, "profile", "", `AWS profile for references without @profile (overrides aws_profile; "" uses the SDK default chain)`)
	f.StringVar(&a.globals.Region, "region", "", "AWS region for all requests (overrides aws_region)")
	f.BoolVarP(&a.globals.Verbose, "verbose", "v", false, "log each request made (never values)")
	f.BoolVar(&a.globals.NoColor, "no-color", false, "disable colored output (also honors NO_COLOR)")

	root.AddCommand(
		a.syncCmd(),
		a.diffCmd(),
		a.listCmd(),
		a.validateCmd(),
		a.getCmd(),
		a.execCmd(),
		a.initCmd(),
		a.versionCmd(),
		a.genCmd(),
	)
	return root
}

// configPath resolves --config, $ENVHAUL_CONFIG, then discovery.
func (a *app) configPath() (string, error) {
	if a.globals.ConfigPath != "" {
		return a.globals.ConfigPath, nil
	}
	if p := os.Getenv("ENVHAUL_CONFIG"); p != "" {
		return p, nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return config.Discover(wd)
}

func (a *app) loadConfig() (*config.Config, error) {
	path, err := a.configPath()
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load(path, config.Options{ValidateRef: providers.Validator().ValidateRef})
	if err != nil {
		var diags config.Diagnostics
		if errors.As(err, &diags) {
			return nil, fmt.Errorf("invalid config:\n%w", err)
		}
		return nil, err
	}
	return cfg, nil
}

func (a *app) registry(cfg *config.Config) *provider.Registry {
	return a.opts.NewRegistry(cfg, &a.globals, a.logger)
}

// colorEnabled reports whether ANSI colors should be written to w.
func (a *app) colorEnabled(w io.Writer) bool {
	if a.globals.NoColor || os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	return a.opts.IsTerminal(w)
}

func (a *app) style(w io.Writer) brand.Style { return brand.DetectStyle(a.colorEnabled(w)) }

func (a *app) paint(w io.Writer, code, text string) string {
	if !a.colorEnabled(w) {
		return text
	}
	return "\x1b[" + code + "m" + text + "\x1b[0m"
}

func (a *app) errorf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(a.opts.Stderr, "%s %s\n", a.paint(a.opts.Stderr, "1;31", "error:"), msg)
}

func (a *app) warnf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(a.opts.Stderr, "%s %s\n", a.paint(a.opts.Stderr, "1;33", "warning:"), msg)
}

// relPath shows p relative to the working directory when it is inside it.
func relPath(p string) string {
	wd, err := os.Getwd()
	if err != nil {
		return p
	}
	if rel, err := filepath.Rel(wd, p); err == nil && !strings.HasPrefix(rel, "..") {
		// Forward slashes match how paths are written in the config, on
		// every OS.
		return filepath.ToSlash(rel)
	}
	return p
}

func isTerminal(v any) bool {
	f, ok := v.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// completeComponents offers component names from the config for shell
// completion.
func (a *app) completeComponents(cmd *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
	cfg, err := a.loadConfig()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	used := map[string]bool{}
	for _, arg := range args {
		used[arg] = true
	}
	var out []string
	for _, c := range cfg.Components {
		if !used[c.Name] {
			desc := c.EnvFile
			if c.Description != "" {
				desc = c.Description
			}
			out = append(out, c.Name+"\t"+desc)
		}
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}
