package pipe

import (
	"strings"
	"testing"

	"github.com/davidtucker/envhaul/internal/template"
)

func calls(t *testing.T, chain string) []template.PipeCall {
	t.Helper()
	tmpl, err := template.Parse("{{ssm:x" + chain + "}}")
	if err != nil {
		t.Fatal(err)
	}
	return tmpl.Refs()[0].Pipes
}

func TestApply(t *testing.T) {
	tests := []struct {
		in, chain, want string
		defaulted       bool
	}{
		{"p@ss w:rd/+&", "|urlencode", "p%40ss%20w%3Ard%2F%2B%26", false},
		{"a-b_c.d~e", "|urlencode", "a-b_c.d~e", false},
		{"hello", "|base64", "aGVsbG8=", false},
		{"aGVsbG8=", "|base64decode", "hello", false},
		{"aGVsbG8", "|base64decode", "hello", false},
		{"  x \n", "|trim", "x", false},
		{"MiXed", "|lower", "mixed", false},
		{"MiXed", "|upper", "MIXED", false},
		{"", "|default:fallback", "fallback", true},
		{"set", "|default:fallback", "set", false},
		{"", "|default:http://a:b@c", "http://a:b@c", true},
		{"   ", "|trim|default:x|upper", "X", true},
		{" A ", "|trim|lower|urlencode", "a", false},
		{"", "", "", false},
	}
	for _, tt := range tests {
		got, defaulted, err := Apply(tt.in, calls(t, tt.chain))
		if err != nil {
			t.Errorf("Apply(%q, %q) error: %v", tt.in, tt.chain, err)
			continue
		}
		if got != tt.want || defaulted != tt.defaulted {
			t.Errorf("Apply(%q, %q) = %q, %v; want %q, %v", tt.in, tt.chain, got, defaulted, tt.want, tt.defaulted)
		}
	}
}

func TestApplyErrors(t *testing.T) {
	tests := []struct{ in, chain, want string }{
		{"!!!", "|base64decode", "not valid base64"},
		{"x", "|nope", "unknown pipe function"},
		{"x", "|trim:arg", "does not take an argument"},
		{"x", "|default", "requires an argument"},
	}
	for _, tt := range tests {
		_, _, err := Apply(tt.in, calls(t, tt.chain))
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("Apply(%q, %q) error = %v, want %q", tt.in, tt.chain, err, tt.want)
		}
	}
}

func TestNamesDescribed(t *testing.T) {
	for _, n := range Names() {
		if Describe(n) == "" {
			t.Errorf("function %q has no description", n)
		}
	}
}
