// Package providers assembles the built-in value providers.
package providers

import (
	"log/slog"

	"github.com/davidtucker/envhaul/internal/awsclient"
	"github.com/davidtucker/envhaul/internal/config"
	"github.com/davidtucker/envhaul/internal/provider"
	"github.com/davidtucker/envhaul/internal/provider/awssecrets"
	"github.com/davidtucker/envhaul/internal/provider/awsssm"
	"github.com/davidtucker/envhaul/internal/provider/azkv"
	"github.com/davidtucker/envhaul/internal/provider/gcpsecrets"
	"github.com/davidtucker/envhaul/internal/provider/onepassword"
	"github.com/davidtucker/envhaul/internal/provider/vault"
	"github.com/davidtucker/envhaul/internal/template"
)

// Options configure the built-in providers.
type Options struct {
	// Config supplies prefixes and AWS defaults. It may be nil for
	// config-independent validation.
	Config *config.Config
	// ProfileOverride and RegionOverride come from --profile and --region.
	// ProfileOverrideSet distinguishes an explicit empty --profile (use the
	// SDK default chain, ignoring aws_profile) from no flag at all.
	ProfileOverride    string
	ProfileOverrideSet bool
	RegionOverride     string
	// AWS creates SDK configs. When nil the providers are offline: they can
	// validate and describe references but not fetch them.
	AWS *awsclient.Cache
	// Online enables fetching for the non-AWS sources (set together with
	// AWS by the CLI).
	Online bool
	Logger *slog.Logger
}

// AWSSettings applies the precedence rules: flags, then config, then the SDK
// defaults (AWS_PROFILE and the profile's region). An explicitly empty
// profile override selects the SDK default chain even when the config sets
// aws_profile.
func AWSSettings(cfg *config.Config, profileOverride string, profileSet bool, regionOverride string) awsclient.Settings {
	s := awsclient.Settings{Profile: profileOverride, Region: regionOverride}
	if cfg != nil {
		if s.Profile == "" && !profileSet {
			s.Profile = cfg.AWSProfile
		}
		if s.Region == "" {
			s.Region = cfg.AWSRegion
		}
	}
	return s
}

// New returns a registry with every built-in provider.
func New(opts Options) *provider.Registry {
	settings := AWSSettings(opts.Config, opts.ProfileOverride, opts.ProfileOverrideSet, opts.RegionOverride)
	var ssmPrefix, secretPrefix string
	if opts.Config != nil {
		ssmPrefix, secretPrefix = opts.Config.SSMPrefix, opts.Config.SecretPrefix
	}
	var ssmClients awsssm.ClientFunc
	var smClients awssecrets.ClientFunc
	if opts.AWS != nil {
		ssmClients = awsssm.ClientsFromCache(opts.AWS)
		smClients = awssecrets.ClientsFromCache(opts.AWS)
	}
	online := opts.Online || opts.AWS != nil
	cfg := opts.Config
	return provider.NewRegistry(
		awsssm.New(awsssm.Options{Prefix: ssmPrefix, Settings: settings, Clients: ssmClients, Logger: opts.Logger}),
		awssecrets.New(awssecrets.Options{Prefix: secretPrefix, Settings: settings, Clients: smClients, Logger: opts.Logger}),
		vault.New(vault.Options{Source: cfg.Source("vault"), Online: online, Logger: opts.Logger}),
		onepassword.New(onepassword.Options{Source: cfg.Source("onepassword"), Online: online, Logger: opts.Logger}),
		gcpsecrets.New(gcpsecrets.Options{Source: cfg.Source("gcp"), Online: online, Logger: opts.Logger}),
		azkv.New(azkv.Options{Source: cfg.Source("azure"), Online: online, Logger: opts.Logger}),
	)
}

// Validator returns an offline registry for checking references against a
// config's source settings.
func Validator(cfg *config.Config) *provider.Registry { return New(Options{Config: cfg}) }

// ValidateRef is a config.Options.ValidateRef that checks references with
// the built-in providers, building one offline registry per config.
func ValidateRef() func(*config.Config, *template.Ref) error {
	var last *config.Config
	var reg *provider.Registry
	return func(cfg *config.Config, ref *template.Ref) error {
		if reg == nil || cfg != last {
			last, reg = cfg, Validator(cfg)
		}
		return reg.ValidateRef(ref)
	}
}
