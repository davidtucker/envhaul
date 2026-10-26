package awsssm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/ssm/types"

	"github.com/davidtucker/envhaul/internal/awsclient"
	"github.com/davidtucker/envhaul/internal/provider"
	"github.com/davidtucker/envhaul/internal/template"
)

type fakeSSM struct {
	mu     sync.Mutex
	params map[string]string
	calls  [][]string
	err    error
}

func (f *fakeSSM) GetParameters(_ context.Context, in *ssm.GetParametersInput, _ ...func(*ssm.Options)) (*ssm.GetParametersOutput, error) {
	f.mu.Lock()
	f.calls = append(f.calls, in.Names)
	f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	if !aws.ToBool(in.WithDecryption) {
		return nil, errors.New("WithDecryption must be set")
	}
	if len(in.Names) > 10 {
		return nil, fmt.Errorf("too many names: %d", len(in.Names))
	}
	out := &ssm.GetParametersOutput{}
	for _, n := range in.Names {
		name, selector, hasSel := strings.Cut(n, ":")
		v, ok := f.params[n]
		if !ok {
			out.InvalidParameters = append(out.InvalidParameters, n)
			continue
		}
		p := types.Parameter{Name: aws.String(name), Value: aws.String(v)}
		if hasSel {
			p.Selector = aws.String(":" + selector)
		}
		out.Parameters = append(out.Parameters, p)
	}
	return out, nil
}

func ref(t *testing.T, s string) *template.Ref {
	t.Helper()
	tmpl, err := template.Parse("{{" + s + "}}")
	if err != nil {
		t.Fatal(err)
	}
	return tmpl.Refs()[0]
}

func TestRequest(t *testing.T) {
	p := New(Options{Prefix: "/app/dev", Settings: awsclient.Settings{Profile: "dev", Region: "us-east-1"}})
	tests := []struct {
		in   string
		want provider.Request
	}{
		{"ssm:api/KEY", provider.Request{Path: "/app/dev/api/KEY", Profile: "dev", Region: "us-east-1"}},
		{"ssm:/abs/KEY@shared", provider.Request{Path: "/abs/KEY", Profile: "shared", Region: "us-east-1"}},
		{"ssm:api/KEY:3", provider.Request{Path: "/app/dev/api/KEY:3", Profile: "dev", Region: "us-east-1"}},
	}
	for _, tt := range tests {
		if got := p.Request(ref(t, tt.in)); got != tt.want {
			t.Errorf("Request(%s) = %+v, want %+v", tt.in, got, tt.want)
		}
	}
	noPrefix := New(Options{})
	if got := noPrefix.Request(ref(t, "ssm:Plain")); got.Path != "Plain" {
		t.Errorf("without prefix, Path = %q", got.Path)
	}
}

func TestValidate(t *testing.T) {
	p := New(Options{})
	if err := p.Validate(ref(t, "ssm:a//b")); err == nil {
		t.Error("expected error for //")
	}
	if err := p.Validate(ref(t, "ssm:a/b")); err != nil {
		t.Error(err)
	}
}

func TestFetchBatchesAndGroups(t *testing.T) {
	clients := map[string]*fakeSSM{
		"dev":    {params: map[string]string{}},
		"shared": {params: map[string]string{"/s/x": "sx", "/s/v:2": "v2"}},
	}
	var reqs []provider.Request
	for i := range 23 {
		name := fmt.Sprintf("/d/p%02d", i)
		if i%2 == 0 {
			clients["dev"].params[name] = "val" + name
		}
		reqs = append(reqs, provider.Request{Path: name, Profile: "dev"})
	}
	reqs = append(reqs,
		provider.Request{Path: "/s/x", Profile: "shared"},
		provider.Request{Path: "/s/v:2", Profile: "shared"},
		provider.Request{Path: "/s/missing", Profile: "shared"},
	)

	p := New(Options{Clients: func(_ context.Context, profile, _ string) (API, error) {
		return clients[profile], nil
	}})
	got, err := p.Fetch(context.Background(), reqs)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(reqs) {
		t.Fatalf("got %d results, want %d", len(got), len(reqs))
	}
	if n := len(clients["dev"].calls); n != 3 {
		t.Errorf("dev: %d GetParameters calls, want 3 (23 names / 10)", n)
	}
	if n := len(clients["shared"].calls); n != 1 {
		t.Errorf("shared: %d calls, want 1", n)
	}
	if r := got[provider.Request{Path: "/d/p00", Profile: "dev"}]; !r.Found || r.Value != "val/d/p00" {
		t.Errorf("p00 = %+v", r)
	}
	if r := got[provider.Request{Path: "/d/p01", Profile: "dev"}]; r.Found {
		t.Errorf("p01 should be missing, got %+v", r)
	}
	if r := got[provider.Request{Path: "/s/v:2", Profile: "shared"}]; !r.Found || r.Value != "v2" {
		t.Errorf("selector param = %+v", r)
	}
	if r := got[provider.Request{Path: "/s/missing", Profile: "shared"}]; r.Found {
		t.Errorf("missing = %+v", r)
	}
}

func TestFetchErrors(t *testing.T) {
	p := New(Options{Clients: func(context.Context, string, string) (API, error) {
		return &fakeSSM{err: errors.New("boom")}, nil
	}})
	_, err := p.Fetch(context.Background(), []provider.Request{{Path: "/a"}})
	if err == nil || !strings.Contains(err.Error(), "boom") || !strings.Contains(err.Error(), "/a") {
		t.Errorf("err = %v", err)
	}

	p = New(Options{Clients: func(context.Context, string, string) (API, error) {
		return nil, errors.New("no creds")
	}})
	if _, err := p.Fetch(context.Background(), []provider.Request{{Path: "/a"}}); err == nil {
		t.Error("expected client error")
	}

	if _, err := New(Options{}).Fetch(context.Background(), nil); err == nil {
		t.Error("offline provider should refuse to fetch")
	}
}

func TestClientsFromCache(t *testing.T) {
	var loads []string
	cache := awsclient.NewCacheWithLoader(func(_ context.Context, profile, region string) (aws.Config, error) {
		loads = append(loads, profile+"/"+region)
		if profile == "broken" {
			return aws.Config{}, errors.New("failed to get shared config profile, broken")
		}
		return aws.Config{Region: "us-east-1", Credentials: aws.AnonymousCredentials{}}, nil
	})
	clients := ClientsFromCache(cache)
	ctx := context.Background()

	a1, err := clients(ctx, "dev", "")
	if err != nil {
		t.Fatal(err)
	}
	a2, _ := clients(ctx, "dev", "")
	b, _ := clients(ctx, "shared", "")
	if a1 != a2 {
		t.Error("the same profile and region should reuse one client")
	}
	if a1 == b {
		t.Error("different profiles should get different clients")
	}
	if _, ok := a1.(*ssm.Client); !ok {
		t.Errorf("client type = %T, want *ssm.Client", a1)
	}
	if got := strings.Join(loads, ","); got != "dev/,shared/" {
		t.Errorf("loads = %s, want one per profile", got)
	}
	if _, err := clients(ctx, "broken", ""); err == nil || !strings.Contains(err.Error(), `profile "broken" not found`) {
		t.Errorf("err = %v, want the explained loader error", err)
	}
}

func TestIdentity(t *testing.T) {
	p := New(Options{})
	if p.Scheme() != "ssm" || p.Description() != "AWS SSM Parameter Store" {
		t.Errorf("Scheme/Description = %q/%q", p.Scheme(), p.Description())
	}
}

func TestValidateRejectsSpaces(t *testing.T) {
	if err := New(Options{}).Validate(ref(t, "ssm:api/my key")); err == nil || !strings.Contains(err.Error(), "spaces") {
		t.Errorf("err = %v", err)
	}
}
