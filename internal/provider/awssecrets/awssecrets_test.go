package awssecrets

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"

	"github.com/davidtucker/envhaul/internal/awsclient"
	"github.com/davidtucker/envhaul/internal/provider"
	"github.com/davidtucker/envhaul/internal/template"
)

type fakeSM struct {
	mu      sync.Mutex
	strings map[string]string
	binary  map[string][]byte
	calls   []string
	err     error
}

func (f *fakeSM) GetSecretValue(_ context.Context, in *secretsmanager.GetSecretValueInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
	id := aws.ToString(in.SecretId)
	f.mu.Lock()
	f.calls = append(f.calls, id)
	f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	if s, ok := f.strings[id]; ok {
		return &secretsmanager.GetSecretValueOutput{SecretString: aws.String(s)}, nil
	}
	if b, ok := f.binary[id]; ok {
		return &secretsmanager.GetSecretValueOutput{SecretBinary: b}, nil
	}
	return nil, &types.ResourceNotFoundException{Message: aws.String("not found")}
}

func TestRequest(t *testing.T) {
	p := New(Options{Prefix: "app/dev/", Settings: awsclient.Settings{Profile: "dev"}})
	parse := func(s string) *template.Ref {
		tmpl, err := template.Parse("{{" + s + "}}")
		if err != nil {
			t.Fatal(err)
		}
		return tmpl.Refs()[0]
	}
	if got := p.Request(parse("secret:db#user")); got != (provider.Request{Path: "app/dev/db", Profile: "dev"}) {
		t.Errorf("got %+v", got)
	}
	arn := "arn:aws:secretsmanager:us-east-1:123456789012:secret:db-AbCdEf"
	if got := p.Request(parse("secret:" + arn + "@other")); got != (provider.Request{Path: arn, Profile: "other"}) {
		t.Errorf("ARN should not be prefixed, got %+v", got)
	}
}

func TestFetch(t *testing.T) {
	fake := &fakeSM{
		strings: map[string]string{"db": `{"user":"u"}`},
		binary:  map[string][]byte{"bin": {0xde, 0xad}},
	}
	p := New(Options{Clients: func(context.Context, string, string) (API, error) { return fake, nil }})
	reqs := []provider.Request{{Path: "db"}, {Path: "bin"}, {Path: "nope"}}
	got, err := p.Fetch(context.Background(), reqs)
	if err != nil {
		t.Fatal(err)
	}
	if r := got[reqs[0]]; !r.Found || r.Value != `{"user":"u"}` {
		t.Errorf("db = %+v", r)
	}
	if r := got[reqs[1]]; !r.Found || r.Value != "3q0=" {
		t.Errorf("bin = %+v, want base64", r)
	}
	if r, ok := got[reqs[2]]; !ok || r.Found {
		t.Errorf("nope = %+v (present=%v), want present and not found", r, ok)
	}
	if len(fake.calls) != 3 {
		t.Errorf("calls = %v", fake.calls)
	}
}

func TestFetchError(t *testing.T) {
	fake := &fakeSM{err: errors.New("throttled")}
	p := New(Options{Clients: func(context.Context, string, string) (API, error) { return fake, nil }})
	_, err := p.Fetch(context.Background(), []provider.Request{{Path: "db"}})
	if err == nil || !strings.Contains(err.Error(), "throttled") {
		t.Errorf("err = %v", err)
	}
}

func TestClientsFromCache(t *testing.T) {
	loads := 0
	cache := awsclient.NewCacheWithLoader(func(_ context.Context, profile, _ string) (aws.Config, error) {
		loads++
		if profile == "broken" {
			return aws.Config{}, errors.New("failed to get shared config profile, broken")
		}
		return aws.Config{Region: "us-east-1", Credentials: aws.AnonymousCredentials{}}, nil
	})
	clients := ClientsFromCache(cache)
	ctx := context.Background()

	a1, err := clients(ctx, "dev", "eu-west-1")
	if err != nil {
		t.Fatal(err)
	}
	a2, _ := clients(ctx, "dev", "eu-west-1")
	b, _ := clients(ctx, "dev", "us-east-1")
	if a1 != a2 {
		t.Error("the same profile and region should reuse one client")
	}
	if a1 == b {
		t.Error("different regions should get different clients")
	}
	if _, ok := a1.(*secretsmanager.Client); !ok {
		t.Errorf("client type = %T, want *secretsmanager.Client", a1)
	}
	if loads != 2 {
		t.Errorf("loader called %d times, want 2", loads)
	}
	if _, err := clients(ctx, "broken", ""); err == nil || !strings.Contains(err.Error(), `profile "broken" not found`) {
		t.Errorf("err = %v, want the explained loader error", err)
	}
}

func TestIdentityAndValidate(t *testing.T) {
	p := New(Options{})
	if p.Scheme() != "secret" || p.Description() != "AWS Secrets Manager" {
		t.Errorf("Scheme/Description = %q/%q", p.Scheme(), p.Description())
	}
	tmpl, err := template.Parse("{{secret:team@corp/key@}}")
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Validate(tmpl.Refs()[0]); err != nil {
		t.Errorf("Validate: %v", err)
	}
}
