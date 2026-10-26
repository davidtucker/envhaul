// Package resolve turns parsed config components into concrete values by
// fetching every reference from its provider and applying pipes.
package resolve

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"golang.org/x/sync/errgroup"

	"github.com/davidtucker/envhaul/internal/config"
	"github.com/davidtucker/envhaul/internal/pipe"
	"github.com/davidtucker/envhaul/internal/provider"
	"github.com/davidtucker/envhaul/internal/template"
)

// Value is one resolved variable.
type Value struct {
	Name  string
	Value string
}

// Component is a resolved component, with values in config order.
type Component struct {
	Component *config.Component
	Values    []Value
}

// Lookup returns the named value.
func (c *Component) Lookup(name string) (string, bool) {
	for _, v := range c.Values {
		if v.Name == name {
			return v.Value, true
		}
	}
	return "", false
}

// Missing describes a reference whose value did not exist (and had no
// default).
type Missing struct {
	Component string
	Var       string
	// Source is the fully qualified reference, e.g. ssm:/app/dev/KEY@prof.
	Source string
	// Reason is set when the value existed but the requested #key did not.
	Reason string
}

func (m Missing) String() string {
	s := fmt.Sprintf("%s.%s: %s not found", m.Component, m.Var, m.Source)
	if m.Reason != "" {
		s = fmt.Sprintf("%s.%s: %s: %s", m.Component, m.Var, m.Source, m.Reason)
	}
	return s
}

// MissingError is returned in strict mode when any value is missing.
type MissingError struct{ Missing []Missing }

func (e *MissingError) Error() string {
	lines := []string{fmt.Sprintf("%d referenced value(s) not found (strict mode):", len(e.Missing))}
	for _, m := range e.Missing {
		lines = append(lines, "  "+m.String())
	}
	return strings.Join(lines, "\n")
}

// Resolver fetches and renders component values.
type Resolver struct {
	Registry *provider.Registry
	// Strict makes any missing value an error.
	Strict bool
}

type fetchKey struct {
	scheme string
	req    provider.Request
}

// Resolve resolves comps. Missing values are rendered as empty strings and
// returned in the second result unless Strict is set.
func (r *Resolver) Resolve(ctx context.Context, comps []*config.Component) ([]*Component, []Missing, error) {
	// Collect unique requests per scheme so each value is fetched once no
	// matter how many variables reference it (with or without #keys).
	byScheme := map[string][]provider.Request{}
	seen := map[fetchKey]bool{}
	for _, c := range comps {
		for _, v := range c.Vars {
			for _, ref := range v.Template.Refs() {
				p, err := r.provider(ref)
				if err != nil {
					return nil, nil, fmt.Errorf("%s.%s: %w", c.Name, v.Name, err)
				}
				k := fetchKey{ref.Scheme, p.Request(ref)}
				if !seen[k] {
					seen[k] = true
					byScheme[k.scheme] = append(byScheme[k.scheme], k.req)
				}
			}
		}
	}

	results := map[fetchKey]provider.Result{}
	if len(byScheme) > 0 {
		type schemeResult struct {
			scheme string
			res    map[provider.Request]provider.Result
		}
		ch := make(chan schemeResult, len(byScheme))
		eg, ectx := errgroup.WithContext(ctx)
		for scheme, reqs := range byScheme {
			p, _ := r.Registry.Get(scheme)
			eg.Go(func() error {
				res, err := p.Fetch(ectx, reqs)
				if err != nil {
					return err
				}
				ch <- schemeResult{scheme, res}
				return nil
			})
		}
		if err := eg.Wait(); err != nil {
			return nil, nil, err
		}
		close(ch)
		for sr := range ch {
			for req, res := range sr.res {
				results[fetchKey{sr.scheme, req}] = res
			}
		}
	}

	var out []*Component
	var missing []Missing
	for _, c := range comps {
		rc := &Component{Component: c}
		for _, v := range c.Vars {
			val, miss, err := r.render(c, v, results)
			if err != nil {
				return nil, nil, err
			}
			missing = append(missing, miss...)
			rc.Values = append(rc.Values, Value{Name: v.Name, Value: val})
		}
		out = append(out, rc)
	}
	if r.Strict && len(missing) > 0 {
		return nil, missing, &MissingError{Missing: missing}
	}
	return out, missing, nil
}

func (r *Resolver) provider(ref *template.Ref) (provider.Provider, error) {
	p, ok := r.Registry.Get(ref.Scheme)
	if !ok {
		return nil, fmt.Errorf("unknown source %q in %s", ref.Scheme, ref.Raw)
	}
	return p, nil
}

func (r *Resolver) render(c *config.Component, v *config.Var, results map[fetchKey]provider.Result) (string, []Missing, error) {
	var b strings.Builder
	var missing []Missing
	for _, part := range v.Template.Parts {
		if part.Ref == nil {
			b.WriteString(part.Literal)
			continue
		}
		ref := part.Ref
		p, err := r.provider(ref)
		if err != nil {
			return "", nil, err
		}
		req := p.Request(ref)
		res := results[fetchKey{ref.Scheme, req}]
		source := provider.Describe(ref.Scheme, req, ref.Key)

		value, found, reason := res.Value, res.Found, ""
		if found && ref.Key != "" {
			value, found, err = jsonKey(res.Value, ref.Key)
			if err != nil {
				return "", nil, fmt.Errorf("%s.%s: %s: %w", c.Name, v.Name, source, err)
			}
			if !found {
				reason = fmt.Sprintf("key %q not present", ref.Key)
			}
		}

		out, defaulted, err := pipe.Apply(value, ref.Pipes)
		if err != nil {
			return "", nil, fmt.Errorf("%s.%s: %s: %w", c.Name, v.Name, source, err)
		}
		if !found && !defaulted {
			missing = append(missing, Missing{Component: c.Name, Var: v.Name, Source: source, Reason: reason})
		}
		b.WriteString(out)
	}
	return b.String(), missing, nil
}

// jsonKey extracts a top-level key from a JSON object. String values are
// returned as-is; other JSON values are returned in compact JSON form.
func jsonKey(doc, key string) (string, bool, error) {
	var obj map[string]json.RawMessage
	dec := json.NewDecoder(strings.NewReader(doc))
	dec.UseNumber()
	if err := dec.Decode(&obj); err != nil || obj == nil {
		return "", false, fmt.Errorf("value is not a JSON object, so #%s cannot be selected", key)
	}
	raw, ok := obj[key]
	if !ok {
		return "", false, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, true, nil
	}
	if string(raw) == "null" {
		return "", true, nil
	}
	var buf bytes.Buffer
	if json.Compact(&buf, raw) == nil {
		return buf.String(), true, nil
	}
	return string(raw), true, nil
}
