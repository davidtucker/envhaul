// Package awsclient loads and caches AWS SDK configurations per named
// profile and region, and turns common credential failures into actionable
// messages.
package awsclient

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/smithy-go"
)

// Settings are the defaults applied to references that do not name a
// profile, plus a region override.
type Settings struct {
	// Profile is the default profile. Empty means the SDK default chain
	// (which honors AWS_PROFILE).
	Profile string
	// Region applies to every request. Empty means each profile's own region.
	Region string
}

// Loader loads an aws.Config for a profile and region.
type Loader func(ctx context.Context, profile, region string) (aws.Config, error)

// Cache memoizes aws.Config values so each profile/region pair resolves its
// credentials once per run.
type Cache struct {
	load Loader

	mu      sync.Mutex
	entries map[key]*entry
}

type key struct{ profile, region string }

type entry struct {
	once sync.Once
	cfg  aws.Config
	err  error
}

// NewCache returns a Cache using the standard SDK shared-config loader.
func NewCache() *Cache { return NewCacheWithLoader(DefaultLoader) }

// NewCacheWithLoader returns a Cache using load.
func NewCacheWithLoader(load Loader) *Cache {
	return &Cache{load: load, entries: map[key]*entry{}}
}

// DefaultLoader loads shared config and credentials (~/.aws/config and
// ~/.aws/credentials), which supports SSO, assume-role, credential_process,
// and endpoint_url overrides.
func DefaultLoader(ctx context.Context, profile, region string) (aws.Config, error) {
	var opts []func(*config.LoadOptions) error
	if profile != "" {
		opts = append(opts, config.WithSharedConfigProfile(profile))
	}
	if region != "" {
		opts = append(opts, config.WithRegion(region))
	}
	return config.LoadDefaultConfig(ctx, opts...)
}

// Config returns the configuration for profile and region.
func (c *Cache) Config(ctx context.Context, profile, region string) (aws.Config, error) {
	k := key{profile, region}
	c.mu.Lock()
	e, ok := c.entries[k]
	if !ok {
		e = &entry{}
		c.entries[k] = e
	}
	c.mu.Unlock()

	e.once.Do(func() {
		e.cfg, e.err = c.load(ctx, profile, region)
		if e.err == nil && e.cfg.Region == "" {
			e.err = fmt.Errorf("no AWS region configured for %s: set aws_region in the config, pass --region, or set region in ~/.aws/config", profileLabel(profile))
		}
		if e.err != nil {
			e.err = Explain(e.err, profile)
		}
	})
	return e.cfg, e.err
}

// Explain rewrites common AWS errors into messages that say how to fix them.
// The original error remains available through errors.Unwrap.
func Explain(err error, profile string) error {
	if err == nil {
		return nil
	}
	var already *explained
	if errors.As(err, &already) {
		return err
	}
	msg := err.Error()
	lower := strings.ToLower(msg)
	login := "aws sso login"
	if profile != "" {
		login += " --profile " + profile
	}

	switch {
	case strings.Contains(lower, "failed to get shared config profile"):
		return &explained{err, fmt.Sprintf("AWS profile %q not found in ~/.aws/config or ~/.aws/credentials", profile)}
	case strings.Contains(lower, "sso") && (strings.Contains(lower, "expired") ||
		strings.Contains(lower, "invalidgrant") || strings.Contains(lower, "unauthorizedexception") ||
		strings.Contains(lower, "failed to read cached sso token") || strings.Contains(lower, "refresh cached sso token")):
		return &explained{err, fmt.Sprintf("AWS SSO session for %s has expired or is missing; run `%s`", profileLabel(profile), login)}
	case strings.Contains(lower, "no ec2 imds role found") || strings.Contains(lower, "failed to retrieve credentials") ||
		strings.Contains(lower, "no valid providers in chain") || strings.Contains(lower, "anonymous credentials"):
		return &explained{err, fmt.Sprintf("no AWS credentials found for %s (configure the profile, or run `%s` for SSO profiles)", profileLabel(profile), login)}
	}

	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "ExpiredToken", "ExpiredTokenException", "RequestExpired":
			return &explained{err, fmt.Sprintf("AWS credentials for %s have expired; refresh them (for SSO: `%s`)", profileLabel(profile), login)}
		case "UnrecognizedClientException", "InvalidClientTokenId", "InvalidSignatureException":
			return &explained{err, fmt.Sprintf("AWS rejected the credentials for %s: %s", profileLabel(profile), apiErr.ErrorMessage())}
		case "AccessDeniedException", "AccessDenied":
			return &explained{err, fmt.Sprintf("access denied using %s: %s", profileLabel(profile), apiErr.ErrorMessage())}
		}
	}
	return err
}

type explained struct {
	err error
	msg string
}

func (e *explained) Error() string { return e.msg }
func (e *explained) Unwrap() error { return e.err }

func profileLabel(profile string) string {
	if profile == "" {
		return "the default AWS profile"
	}
	return fmt.Sprintf("profile %q", profile)
}
