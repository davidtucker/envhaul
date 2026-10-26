// Package onepassword reads secrets from 1Password through the 1Password CLI
// (op), reusing whatever sign-in op already has: the desktop app
// integration, `op signin`, or a service account token in CI.
package onepassword

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"regexp"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/davidtucker/envhaul/internal/config"
	"github.com/davidtucker/envhaul/internal/provider"
	"github.com/davidtucker/envhaul/internal/template"
)

// Scheme is the reference scheme, as in {{op:vault/item/field}}.
const Scheme = "op"

// Runner runs the op CLI with args and returns its stdout and stderr.
type Runner func(ctx context.Context, args []string) (stdout, stderr []byte, err error)

// Options configure the provider.
type Options struct {
	// Source is the config's onepassword block. It may be nil.
	Source *config.Source
	// Online enables Fetch.
	Online bool
	// Run replaces the op CLI (tests).
	Run         Runner
	Concurrency int
	Logger      *slog.Logger
}

// Provider implements provider.Provider for 1Password.
type Provider struct{ opts Options }

// New returns a 1Password provider.
func New(opts Options) *Provider {
	if opts.Concurrency <= 0 {
		opts.Concurrency = 4
	}
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	if opts.Run == nil {
		opts.Run = runOp
	}
	return &Provider{opts: opts}
}

func runOp(ctx context.Context, args []string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, "op", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

func (p *Provider) Scheme() string      { return Scheme }
func (p *Provider) Description() string { return "1Password" }

// Validate checks for op's vault/item/field or vault/item/section/field form.
func (p *Provider) Validate(ref *template.Ref) error {
	path := strings.TrimPrefix(ref.Path, "op://")
	parts := strings.Split(path, "/")
	if len(parts) < 3 || len(parts) > 4 {
		return fmt.Errorf("1Password reference %q must be vault/item/field or vault/item/section/field", ref.Path)
	}
	for _, part := range parts {
		if part == "" {
			return fmt.Errorf("1Password reference %q has an empty segment", ref.Path)
		}
	}
	return nil
}

// Request normalizes an optional op:// prefix and resolves the account.
func (p *Provider) Request(ref *template.Ref) provider.Request {
	return provider.Request{Path: strings.TrimPrefix(ref.Path, "op://"), Profile: ref.Profile}
}

// account maps @profile to an account: a configured profile's account, or
// the name itself; without a profile, the block's default account.
func (p *Provider) account(profile string) string {
	src := p.opts.Source
	if profile != "" && !src.HasProfile(profile) {
		return profile
	}
	return src.Setting(profile, "account")
}

// Fetch runs `op read` per reference. The first read for each account runs
// alone so the 1Password app asks to authorize at most once; the rest run
// concurrently.
func (p *Provider) Fetch(ctx context.Context, reqs []provider.Request) (map[provider.Request]provider.Result, error) {
	if !p.opts.Online {
		return nil, errors.New("1Password provider is not configured for remote access")
	}
	out := make(map[provider.Request]provider.Result, len(reqs))
	var rest []provider.Request
	first := map[string]bool{}
	for _, r := range reqs {
		acct := p.account(r.Profile)
		if first[acct] {
			rest = append(rest, r)
			continue
		}
		first[acct] = true
		res, err := p.read(ctx, r)
		if err != nil {
			return nil, err
		}
		out[r] = res
	}

	var mu sync.Mutex
	eg, ctx := errgroup.WithContext(ctx)
	eg.SetLimit(p.opts.Concurrency)
	for _, r := range rest {
		eg.Go(func() error {
			res, err := p.read(ctx, r)
			if err != nil {
				return err
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

var (
	notFoundRe  = regexp.MustCompile(`(?i)isn't an item|isn't a vault|isn't a field|does not have a field|could not find|no item found|not found in`)
	notSignedRe = regexp.MustCompile(`(?i)not currently signed in|not signed in|session expired|authorization (?:prompt )?(?:dismissed|denied)|unlock`)
	logPrefixRe = regexp.MustCompile(`^\[ERROR\] [0-9/]+ [0-9:]+ `)
)

func (p *Provider) read(ctx context.Context, r provider.Request) (provider.Result, error) {
	args := []string{"read", "--no-newline"}
	acct := p.account(r.Profile)
	if acct != "" {
		args = append(args, "--account", acct)
	}
	args = append(args, "op://"+r.Path)
	p.opts.Logger.Debug("op read", "account", acct, "reference", "op://"+r.Path)

	stdout, stderr, err := p.opts.Run(ctx, args)
	if err == nil {
		return provider.Result{Value: string(stdout), Found: true}, nil
	}
	if errors.Is(err, exec.ErrNotFound) {
		return provider.Result{}, errors.New("1Password CLI (op) not found on PATH: install it from https://developer.1password.com/docs/cli/get-started/")
	}
	msg := strings.TrimSpace(logPrefixRe.ReplaceAllString(strings.TrimSpace(string(stderr)), ""))
	switch {
	case notFoundRe.MatchString(msg):
		return provider.Result{}, nil
	case notSignedRe.MatchString(msg):
		label := "the default account"
		if acct != "" {
			label = fmt.Sprintf("account %q", acct)
		}
		return provider.Result{}, fmt.Errorf("1Password: not signed in to %s: unlock the 1Password app (with CLI integration on) or run `op signin`; in CI set OP_SERVICE_ACCOUNT_TOKEN", label)
	case msg == "":
		msg = err.Error()
	}
	return provider.Result{}, fmt.Errorf("1Password: reading op://%s: %s", r.Path, msg)
}
