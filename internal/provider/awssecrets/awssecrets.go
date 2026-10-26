// Package awssecrets reads values from AWS Secrets Manager.
package awssecrets

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
	"golang.org/x/sync/errgroup"

	"github.com/davidtucker/envhaul/internal/awsclient"
	"github.com/davidtucker/envhaul/internal/provider"
	"github.com/davidtucker/envhaul/internal/template"
)

// Scheme is the reference scheme, as in {{secret:id#key}}.
const Scheme = "secret"

// API is the subset of the Secrets Manager client used here.
type API interface {
	GetSecretValue(ctx context.Context, in *secretsmanager.GetSecretValueInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error)
}

// ClientFunc returns a client for a profile and region.
type ClientFunc func(ctx context.Context, profile, region string) (API, error)

// Options configure the provider.
type Options struct {
	// Prefix is prepended to secret ids that are not ARNs.
	Prefix   string
	Settings awsclient.Settings
	// Clients creates clients. It may be nil for offline use.
	Clients     ClientFunc
	Concurrency int
	Logger      *slog.Logger
}

// Provider implements provider.Provider for Secrets Manager.
type Provider struct{ opts Options }

// New returns a Secrets Manager provider.
func New(opts Options) *Provider {
	if opts.Concurrency <= 0 {
		opts.Concurrency = 8
	}
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	return &Provider{opts: opts}
}

// ClientsFromCache returns a ClientFunc backed by an awsclient.Cache.
func ClientsFromCache(c *awsclient.Cache) ClientFunc {
	var mu sync.Mutex
	clients := map[[2]string]API{}
	return func(ctx context.Context, profile, region string) (API, error) {
		mu.Lock()
		defer mu.Unlock()
		if cl, ok := clients[[2]string{profile, region}]; ok {
			return cl, nil
		}
		cfg, err := c.Config(ctx, profile, region)
		if err != nil {
			return nil, err
		}
		cl := secretsmanager.NewFromConfig(cfg)
		clients[[2]string{profile, region}] = cl
		return cl, nil
	}
}

func (p *Provider) Scheme() string      { return Scheme }
func (p *Provider) Description() string { return "AWS Secrets Manager" }

func (p *Provider) Validate(ref *template.Ref) error { return nil }

// Request applies the prefix (except to ARNs) and default profile.
func (p *Provider) Request(ref *template.Ref) provider.Request {
	id := ref.Path
	if p.opts.Prefix != "" && !strings.HasPrefix(id, "arn:") {
		id = p.opts.Prefix + id
	}
	profile := ref.Profile
	if profile == "" {
		profile = p.opts.Settings.Profile
	}
	return provider.Request{Path: id, Profile: profile, Region: p.opts.Settings.Region}
}

// Fetch calls GetSecretValue once per unique secret. Binary secrets are
// returned base64-encoded.
func (p *Provider) Fetch(ctx context.Context, reqs []provider.Request) (map[provider.Request]provider.Result, error) {
	if p.opts.Clients == nil {
		return nil, fmt.Errorf("secret provider is not configured for remote access")
	}
	var mu sync.Mutex
	out := make(map[provider.Request]provider.Result, len(reqs))
	eg, ctx := errgroup.WithContext(ctx)
	eg.SetLimit(p.opts.Concurrency)
	for _, r := range reqs {
		client, err := p.opts.Clients(ctx, r.Profile, r.Region)
		if err != nil {
			_ = eg.Wait()
			return nil, err
		}
		eg.Go(func() error {
			p.opts.Logger.Debug("secretsmanager GetSecretValue", "profile", r.Profile, "region", r.Region, "id", r.Path)
			resp, err := client.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{SecretId: aws.String(r.Path)})
			var res provider.Result
			switch {
			case err == nil:
				res.Found = true
				if resp.SecretString != nil {
					res.Value = *resp.SecretString
				} else {
					res.Value = base64.StdEncoding.EncodeToString(resp.SecretBinary)
				}
			case isNotFound(err):
				// Missing: leave Found=false.
			default:
				return fmt.Errorf("secretsmanager: reading %s: %w", r.Path, awsclient.Explain(err, r.Profile))
			}
			mu.Lock()
			out[r] = res
			mu.Unlock()
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return nil, err
	}
	return out, nil
}

func isNotFound(err error) bool {
	var nf *types.ResourceNotFoundException
	return errors.As(err, &nf)
}
