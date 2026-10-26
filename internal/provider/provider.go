// Package provider defines the interface every value source implements and
// a registry that maps reference schemes to providers.
//
// Adding a new source means implementing Provider in its own package and
// registering it in internal/providers.
package provider

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/davidtucker/envhaul/internal/template"
)

// Request identifies a single value to fetch. It is comparable and used as a
// map key so that identical references are fetched once.
type Request struct {
	// Path is the fully qualified identifier (prefixes applied).
	Path string
	// Profile is the credential profile to use. Empty means the provider's
	// default credential chain.
	Profile string
	// Region is the region to use. Empty means the profile's default.
	Region string
}

// Result is the outcome of fetching one Request.
type Result struct {
	Value string
	// Found is false when the value does not exist at the source. Missing
	// values are not errors; callers decide whether to warn or fail.
	Found bool
}

// Provider is a source of values for one reference scheme.
type Provider interface {
	// Scheme is the reference prefix this provider handles, e.g. "ssm".
	Scheme() string
	// Description is a short human-readable name, e.g. "AWS SSM Parameter Store".
	Description() string
	// Validate performs offline checks on a reference.
	Validate(ref *template.Ref) error
	// Request converts a reference into a fully qualified request, applying
	// prefixes and default profile and region.
	Request(ref *template.Ref) Request
	// Fetch retrieves values. It must return a Result for every request.
	// Errors are reserved for failures such as missing credentials or denied
	// access; values that do not exist are reported with Found=false.
	Fetch(ctx context.Context, reqs []Request) (map[Request]Result, error)
}

// Registry maps schemes to providers.
type Registry struct {
	providers map[string]Provider
}

// NewRegistry returns a registry containing ps.
func NewRegistry(ps ...Provider) *Registry {
	r := &Registry{providers: map[string]Provider{}}
	for _, p := range ps {
		r.providers[p.Scheme()] = p
	}
	return r
}

// Get returns the provider for scheme.
func (r *Registry) Get(scheme string) (Provider, bool) {
	p, ok := r.providers[scheme]
	return p, ok
}

// Schemes returns the registered schemes, sorted.
func (r *Registry) Schemes() []string {
	out := make([]string, 0, len(r.providers))
	for s := range r.providers {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// ValidateRef checks that ref's scheme is registered and that the provider
// accepts it. It is suitable for config.Options.ValidateRef.
func (r *Registry) ValidateRef(ref *template.Ref) error {
	p, ok := r.Get(ref.Scheme)
	if !ok {
		return fmt.Errorf("unknown source %q in %s (available: %s)", ref.Scheme, ref.Raw, strings.Join(r.Schemes(), ", "))
	}
	return p.Validate(ref)
}

// Describe formats a request the way users write references, for logs and
// messages: scheme:path[#key][@profile].
func Describe(scheme string, req Request, key string) string {
	s := scheme + ":" + req.Path
	if key != "" {
		s += "#" + key
	}
	if req.Profile != "" {
		s += "@" + req.Profile
	}
	return s
}
