package provider

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/davidtucker/envhaul/internal/template"
)

type stub struct {
	scheme string
	err    error
}

func (s stub) Scheme() string                                               { return s.scheme }
func (s stub) Description() string                                          { return "stub " + s.scheme }
func (s stub) Validate(*template.Ref) error                                 { return s.err }
func (s stub) Request(r *template.Ref) Request                              { return Request{Path: r.Path} }
func (s stub) Fetch(context.Context, []Request) (map[Request]Result, error) { return nil, nil }

func ref(t *testing.T, s string) *template.Ref {
	t.Helper()
	tmpl, err := template.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return tmpl.Refs()[0]
}

func TestRegistry(t *testing.T) {
	r := NewRegistry(stub{scheme: "zeta"}, stub{scheme: "alpha", err: errors.New("bad path")})
	if got := strings.Join(r.Schemes(), ","); got != "alpha,zeta" {
		t.Errorf("Schemes() = %s, want sorted", got)
	}
	if p, ok := r.Get("zeta"); !ok || p.Scheme() != "zeta" {
		t.Errorf("Get(zeta) = %v, %v", p, ok)
	}
	if _, ok := r.Get("nope"); ok {
		t.Error("Get(nope) should not find a provider")
	}

	if err := r.ValidateRef(ref(t, "{{zeta:x}}")); err != nil {
		t.Errorf("valid ref: %v", err)
	}
	if err := r.ValidateRef(ref(t, "{{alpha:x}}")); err == nil || err.Error() != "bad path" {
		t.Errorf("provider validation error not passed through: %v", err)
	}
	err := r.ValidateRef(ref(t, "{{vault:x}}"))
	if err == nil || !strings.Contains(err.Error(), `unknown source "vault" in {{vault:x}} (available: alpha, zeta)`) {
		t.Errorf("unknown scheme error = %v", err)
	}
}

func TestDescribe(t *testing.T) {
	tests := []struct {
		req  Request
		key  string
		want string
	}{
		{Request{Path: "/a/b"}, "", "ssm:/a/b"},
		{Request{Path: "/a/b", Profile: "prod"}, "", "ssm:/a/b@prod"},
		{Request{Path: "db", Profile: "prod"}, "password", "ssm:db#password@prod"},
	}
	for _, tt := range tests {
		if got := Describe("ssm", tt.req, tt.key); got != tt.want {
			t.Errorf("Describe(%+v, %q) = %q, want %q", tt.req, tt.key, got, tt.want)
		}
	}
}
