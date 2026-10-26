// Package pipe implements the transformation functions that can be chained
// onto a reference with "|", e.g. {{secret:db#password|trim|urlencode}}.
package pipe

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/davidtucker/envhaul/internal/template"
)

type argMode int

const (
	argNone argMode = iota
	argRequired
)

type function struct {
	desc string
	arg  argMode
	fn   func(in, arg string) (string, error)
}

var functions = map[string]function{
	"urlencode": {
		desc: "percent-encode every character except A-Z a-z 0-9 - _ . ~",
		fn: func(in, _ string) (string, error) {
			// QueryEscape encodes spaces as "+", which is only valid in query
			// strings; %20 is correct everywhere (userinfo, paths, queries).
			return strings.ReplaceAll(url.QueryEscape(in), "+", "%20"), nil
		},
	},
	"base64": {
		desc: "standard base64 encode",
		fn: func(in, _ string) (string, error) {
			return base64.StdEncoding.EncodeToString([]byte(in)), nil
		},
	},
	"base64decode": {
		desc: "standard base64 decode (padded or unpadded)",
		fn: func(in, _ string) (string, error) {
			s := strings.TrimSpace(in)
			b, err := base64.StdEncoding.DecodeString(s)
			if err != nil {
				b, err = base64.RawStdEncoding.DecodeString(s)
			}
			if err != nil {
				return "", fmt.Errorf("base64decode: input is not valid base64")
			}
			return string(b), nil
		},
	},
	"trim": {
		desc: "remove leading and trailing whitespace",
		fn:   func(in, _ string) (string, error) { return strings.TrimSpace(in), nil },
	},
	"lower": {
		desc: "convert to lowercase",
		fn:   func(in, _ string) (string, error) { return strings.ToLower(in), nil },
	},
	"upper": {
		desc: "convert to uppercase",
		fn:   func(in, _ string) (string, error) { return strings.ToUpper(in), nil },
	},
	"default": {
		desc: "use the argument if the value is empty or missing (default:value)",
		arg:  argRequired,
		fn: func(in, arg string) (string, error) {
			if in == "" {
				return arg, nil
			}
			return in, nil
		},
	},
}

// Names returns the available function names, sorted.
func Names() []string {
	names := make([]string, 0, len(functions))
	for n := range functions {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Describe returns a one-line description of the named function.
func Describe(name string) string { return functions[name].desc }

// Validate checks that the call names a known function with the right
// arguments.
func Validate(c template.PipeCall) error {
	f, ok := functions[c.Name]
	if !ok {
		return fmt.Errorf("unknown pipe function %q (available: %s)", c.Name, strings.Join(Names(), ", "))
	}
	switch {
	case f.arg == argNone && c.HasArg:
		return fmt.Errorf("pipe function %q does not take an argument", c.Name)
	case f.arg == argRequired && !c.HasArg:
		return fmt.Errorf("pipe function %q requires an argument (%s:value)", c.Name, c.Name)
	}
	return nil
}

// Apply runs calls over value in order. It reports whether a "default"
// supplied the final value, which callers use to decide whether a missing
// source value should be reported.
func Apply(value string, calls []template.PipeCall) (out string, defaulted bool, err error) {
	out = value
	for _, c := range calls {
		if err := Validate(c); err != nil {
			return "", false, err
		}
		before := out
		out, err = functions[c.Name].fn(out, c.Arg)
		if err != nil {
			return "", false, err
		}
		if c.Name == "default" && before == "" {
			defaulted = true
		}
	}
	return out, defaulted, nil
}
