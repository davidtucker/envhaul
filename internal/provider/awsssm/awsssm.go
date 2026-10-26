// Package awsssm reads values from AWS Systems Manager Parameter Store.
package awsssm

import (
	"context"
	"fmt"
	"log/slog"
	"path"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"golang.org/x/sync/errgroup"

	"github.com/davidtucker/envhaul/internal/awsclient"
	"github.com/davidtucker/envhaul/internal/provider"
	"github.com/davidtucker/envhaul/internal/template"
)

// Scheme is the reference scheme, as in {{ssm:path}}.
const Scheme = "ssm"

// batchSize is the maximum number of names GetParameters accepts.
const batchSize = 10

// API is the subset of the SSM client used here.
type API interface {
	GetParameters(ctx context.Context, in *ssm.GetParametersInput, optFns ...func(*ssm.Options)) (*ssm.GetParametersOutput, error)
}

// ClientFunc returns a client for a profile and region.
type ClientFunc func(ctx context.Context, profile, region string) (API, error)

// Options configure the provider.
type Options struct {
	// Prefix is prepended to paths that do not start with "/".
	Prefix   string
	Settings awsclient.Settings
	// Clients creates SSM clients. It may be nil for offline use (validate,
	// list), in which case Fetch fails.
	Clients ClientFunc
	// Concurrency bounds the number of in-flight API calls.
	Concurrency int
	Logger      *slog.Logger
}

// Provider implements provider.Provider for SSM Parameter Store.
type Provider struct{ opts Options }

// New returns an SSM provider.
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
		cl := ssm.NewFromConfig(cfg)
		clients[[2]string{profile, region}] = cl
		return cl, nil
	}
}

func (p *Provider) Scheme() string      { return Scheme }
func (p *Provider) Description() string { return "AWS SSM Parameter Store" }

func (p *Provider) Validate(ref *template.Ref) error {
	if strings.Contains(ref.Path, "//") {
		return fmt.Errorf("SSM parameter path %q must not contain //", ref.Path)
	}
	return nil
}

// Request applies the prefix and default profile. Paths starting with "/"
// are absolute; anything else is joined onto the prefix.
func (p *Provider) Request(ref *template.Ref) provider.Request {
	name := ref.Path
	if !strings.HasPrefix(name, "/") && p.opts.Prefix != "" {
		name = path.Join(p.opts.Prefix, name)
	}
	profile := ref.Profile
	if profile == "" {
		profile = p.opts.Settings.Profile
	}
	return provider.Request{Path: name, Profile: profile, Region: p.opts.Settings.Region}
}

// Fetch retrieves parameters with decryption, batching up to ten names per
// call and grouping by profile and region.
func (p *Provider) Fetch(ctx context.Context, reqs []provider.Request) (map[provider.Request]provider.Result, error) {
	if p.opts.Clients == nil {
		return nil, fmt.Errorf("ssm provider is not configured for remote access")
	}
	type group struct{ profile, region string }
	groups := map[group][]provider.Request{}
	var order []group
	for _, r := range reqs {
		g := group{r.Profile, r.Region}
		if _, ok := groups[g]; !ok {
			order = append(order, g)
		}
		groups[g] = append(groups[g], r)
	}

	var mu sync.Mutex
	out := make(map[provider.Request]provider.Result, len(reqs))
	eg, ctx := errgroup.WithContext(ctx)
	eg.SetLimit(p.opts.Concurrency)
	for _, g := range order {
		client, err := p.opts.Clients(ctx, g.profile, g.region)
		if err != nil {
			_ = eg.Wait()
			return nil, err
		}
		batch := groups[g]
		for start := 0; start < len(batch); start += batchSize {
			chunk := batch[start:min(start+batchSize, len(batch))]
			eg.Go(func() error {
				names := make([]string, len(chunk))
				for i, r := range chunk {
					names[i] = r.Path
				}
				p.opts.Logger.Debug("ssm GetParameters", "profile", g.profile, "region", g.region, "names", names)
				resp, err := client.GetParameters(ctx, &ssm.GetParametersInput{
					Names:          names,
					WithDecryption: aws.Bool(true),
				})
				if err != nil {
					return fmt.Errorf("ssm: reading %s: %w", strings.Join(names, ", "), awsclient.Explain(err, g.profile))
				}
				found := make(map[string]string, len(resp.Parameters))
				for _, prm := range resp.Parameters {
					// Selectors (name:3, name:label) come back separately from
					// the bare name.
					found[aws.ToString(prm.Name)+aws.ToString(prm.Selector)] = aws.ToString(prm.Value)
				}
				mu.Lock()
				defer mu.Unlock()
				for _, r := range chunk {
					v, ok := found[r.Path]
					out[r] = provider.Result{Value: v, Found: ok}
				}
				return nil
			})
		}
	}
	if err := eg.Wait(); err != nil {
		return nil, err
	}
	return out, nil
}
