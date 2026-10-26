package awsclient

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/smithy-go"
)

func TestCacheLoadsOncePerKey(t *testing.T) {
	var loads atomic.Int32
	c := NewCacheWithLoader(func(_ context.Context, profile, region string) (aws.Config, error) {
		loads.Add(1)
		return aws.Config{Region: "us-east-1"}, nil
	})
	for range 5 {
		if _, err := c.Config(context.Background(), "dev", ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := c.Config(context.Background(), "prod", ""); err != nil {
		t.Fatal(err)
	}
	if n := loads.Load(); n != 2 {
		t.Errorf("loader called %d times, want 2", n)
	}
}

func TestCacheRequiresRegion(t *testing.T) {
	c := NewCacheWithLoader(func(context.Context, string, string) (aws.Config, error) {
		return aws.Config{}, nil
	})
	_, err := c.Config(context.Background(), "dev", "")
	if err == nil || !strings.Contains(err.Error(), "no AWS region") || !strings.Contains(err.Error(), `profile "dev"`) {
		t.Errorf("err = %v", err)
	}
}

func TestExplain(t *testing.T) {
	tests := []struct {
		err     error
		profile string
		want    string
	}{
		{errors.New("failed to get shared config profile, acme-dev"), "acme-dev", `AWS profile "acme-dev" not found`},
		{errors.New("operation error SSO: GetRoleCredentials, https response error StatusCode: 401, UnauthorizedException: Session token not found or invalid"), "acme-dev", "aws sso login --profile acme-dev"},
		{errors.New("refresh cached SSO token failed, unable to refresh SSO token, InvalidGrantException"), "", "run `aws sso login`"},
		{errors.New("failed to refresh cached credentials, no EC2 IMDS role found"), "", "no AWS credentials found for the default AWS profile"},
		{&smithy.GenericAPIError{Code: "AccessDeniedException", Message: "User is not authorized to perform: ssm:GetParameters"}, "p", `access denied using profile "p": User is not authorized`},
		{&smithy.GenericAPIError{Code: "ExpiredTokenException", Message: "expired"}, "p", "have expired"},
	}
	for _, tt := range tests {
		got := Explain(tt.err, tt.profile)
		if !strings.Contains(got.Error(), tt.want) {
			t.Errorf("Explain(%q) = %q, want it to contain %q", tt.err, got, tt.want)
		}
		if !errors.Is(got, tt.err) {
			t.Errorf("Explain(%q) does not wrap the original error", tt.err)
		}
	}
	other := errors.New("something else")
	if Explain(other, "") != other { //nolint:errorlint // identity is the point
		t.Error("unrecognized errors should be returned unchanged")
	}
	if Explain(nil, "") != nil {
		t.Error("Explain(nil) should be nil")
	}
	// Explaining twice must not double-wrap.
	once := Explain(errors.New("failed to get shared config profile, x"), "x")
	if Explain(once, "x") != once { //nolint:errorlint // identity is the point
		t.Error("Explain should be idempotent")
	}
}
