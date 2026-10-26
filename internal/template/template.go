// Package template parses envhaul value templates.
//
// A template is a string that may contain any number of references of the
// form:
//
//	{{ scheme:path[#key][@profile][|fn[:arg]]... }}
//
// Text outside references is copied through literally. A literal "{{" is
// written as "\{{".
package template

import (
	"fmt"
	"regexp"
	"strings"
)

// Template is a parsed value: an ordered sequence of literal text and
// references.
type Template struct {
	Raw   string
	Parts []Part
}

// Part is either literal text or a reference. Exactly one field is set.
type Part struct {
	Literal string
	Ref     *Ref
}

// Ref is a single {{...}} reference to an external value.
type Ref struct {
	// Scheme selects the provider, e.g. "ssm" or "secret".
	Scheme string
	// Path is the provider-specific identifier as written (before any prefix
	// is applied).
	Path string
	// Key, if set, selects a top-level key from a JSON value.
	Key string
	// Profile, if set, overrides the default credential profile.
	Profile string
	// Pipes are transformations applied, in order, to the resolved value.
	Pipes []PipeCall
	// Raw is the full reference text including braces.
	Raw string
	// Offset is the byte offset of Raw within the template.
	Offset int
}

// PipeCall is one "|name" or "|name:arg" step.
type PipeCall struct {
	Name   string
	Arg    string
	HasArg bool
}

// String formats the call the way it is written in a template.
func (p PipeCall) String() string {
	if p.HasArg {
		return p.Name + ":" + p.Arg
	}
	return p.Name
}

// Error is a parse error with the byte offset at which it occurred.
type Error struct {
	Offset int
	Msg    string
}

func (e *Error) Error() string { return e.Msg }

var (
	schemeRe  = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)
	profileRe = regexp.MustCompile(`^[A-Za-z0-9_.+=,@-]+$`)
	pipeRe    = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
)

// Parse parses s into a Template. Strings without references produce a single
// literal part (or none, for the empty string).
func Parse(s string) (*Template, error) {
	t := &Template{Raw: s}
	var lit strings.Builder
	flush := func() {
		if lit.Len() > 0 {
			t.Parts = append(t.Parts, Part{Literal: lit.String()})
			lit.Reset()
		}
	}

	for i := 0; i < len(s); {
		switch {
		case strings.HasPrefix(s[i:], `\{{`):
			lit.WriteString("{{")
			i += 3
		case strings.HasPrefix(s[i:], "{{"):
			end := strings.Index(s[i+2:], "}}")
			if end < 0 {
				return nil, &Error{Offset: i, Msg: "unterminated reference: missing closing }}"}
			}
			inner := s[i+2 : i+2+end]
			if j := strings.Index(inner, "{{"); j >= 0 {
				return nil, &Error{Offset: i + 2 + j, Msg: "nested {{ inside a reference"}
			}
			if j := strings.IndexAny(inner, "{}"); j >= 0 {
				return nil, &Error{Offset: i + 2 + j, Msg: "braces are not allowed inside a reference"}
			}
			ref, err := parseRef(inner, i)
			if err != nil {
				return nil, err
			}
			ref.Raw = s[i : i+2+end+2]
			flush()
			t.Parts = append(t.Parts, Part{Ref: ref})
			i += 2 + end + 2
		default:
			lit.WriteByte(s[i])
			i++
		}
	}
	flush()
	return t, nil
}

func parseRef(inner string, offset int) (*Ref, error) {
	errf := func(format string, args ...any) error {
		return &Error{Offset: offset, Msg: fmt.Sprintf(format, args...)}
	}

	segments := strings.Split(inner, "|")
	spec := strings.TrimSpace(segments[0])
	if spec == "" {
		return nil, errf("empty reference")
	}

	scheme, rest, ok := strings.Cut(spec, ":")
	if !ok {
		return nil, errf("reference %q is missing a scheme (expected scheme:path, e.g. ssm:my/param)", spec)
	}
	scheme = strings.TrimSpace(scheme)
	if !schemeRe.MatchString(scheme) {
		return nil, errf("invalid scheme %q", scheme)
	}
	ref := &Ref{Scheme: scheme, Offset: offset}

	// The profile is split on the last "@" because some identifiers (Secrets
	// Manager names) may themselves contain "@". A trailing "@" with nothing
	// after it means "default profile" and lets such identifiers be written
	// unambiguously.
	if at := strings.LastIndex(rest, "@"); at >= 0 {
		ref.Profile = strings.TrimSpace(rest[at+1:])
		rest = rest[:at]
		if ref.Profile != "" && !profileRe.MatchString(ref.Profile) {
			return nil, errf("invalid profile name %q", ref.Profile)
		}
	}
	if hash := strings.Index(rest, "#"); hash >= 0 {
		ref.Key = strings.TrimSpace(rest[hash+1:])
		rest = rest[:hash]
		if ref.Key == "" {
			return nil, errf("empty key after # in %q", spec)
		}
	}
	ref.Path = strings.TrimSpace(rest)
	if ref.Path == "" {
		return nil, errf("empty path in %q", spec)
	}
	if strings.ContainsAny(ref.Path, " \t\r\n") {
		return nil, errf("path %q must not contain whitespace", ref.Path)
	}

	for _, seg := range segments[1:] {
		seg = strings.TrimSpace(seg)
		name, arg, hasArg := strings.Cut(seg, ":")
		name = strings.TrimSpace(name)
		if !pipeRe.MatchString(name) {
			return nil, errf("invalid pipe function %q", seg)
		}
		ref.Pipes = append(ref.Pipes, PipeCall{Name: name, Arg: arg, HasArg: hasArg})
	}
	return ref, nil
}

// Refs returns the template's references in order.
func (t *Template) Refs() []*Ref {
	var refs []*Ref
	for _, p := range t.Parts {
		if p.Ref != nil {
			refs = append(refs, p.Ref)
		}
	}
	return refs
}

// IsLiteral reports whether the template contains no references.
func (t *Template) IsLiteral() bool {
	for _, p := range t.Parts {
		if p.Ref != nil {
			return false
		}
	}
	return true
}

// Literal returns the template's text with escapes processed. It is only
// meaningful when IsLiteral is true.
func (t *Template) Literal() string {
	var b strings.Builder
	for _, p := range t.Parts {
		b.WriteString(p.Literal)
	}
	return b.String()
}

// String formats the reference canonically, without braces or whitespace.
func (r *Ref) String() string {
	var b strings.Builder
	b.WriteString(r.Scheme)
	b.WriteByte(':')
	b.WriteString(r.Path)
	if r.Key != "" {
		b.WriteByte('#')
		b.WriteString(r.Key)
	}
	if r.Profile != "" {
		b.WriteByte('@')
		b.WriteString(r.Profile)
	}
	for _, p := range r.Pipes {
		b.WriteByte('|')
		b.WriteString(p.String())
	}
	return b.String()
}
