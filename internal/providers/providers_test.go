package providers

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"

	"github.com/davidtucker/envhaul/internal/awsclient"
	"github.com/davidtucker/envhaul/internal/config"
	"github.com/davidtucker/envhaul/internal/provider"
)

func TestAWSSettings(t *testing.T) {
	cfg := &config.Config{AWSProfile: "cfg-profile", AWSRegion: "cfg-region"}
	tests := []struct {
		name            string
		cfg             *config.Config
		profile, region string
		profileSet      bool
		want            awsclient.Settings
	}{
		{"config defaults", cfg, "", "", false, awsclient.Settings{Profile: "cfg-profile", Region: "cfg-region"}},
		{"flags win", cfg, "flag", "eu-west-1", true, awsclient.Settings{Profile: "flag", Region: "eu-west-1"}},
		{"explicit empty profile", cfg, "", "", true, awsclient.Settings{Region: "cfg-region"}},
		{"no config", nil, "", "", false, awsclient.Settings{}},
	}
	for _, tt := range tests {
		if got := AWSSettings(tt.cfg, tt.profile, tt.profileSet, tt.region); got != tt.want {
			t.Errorf("%s: got %+v, want %+v", tt.name, got, tt.want)
		}
	}
}

func TestValidatorKnowsBuiltins(t *testing.T) {
	if got := Validator().Schemes(); len(got) != 2 || got[0] != "secret" || got[1] != "ssm" {
		t.Errorf("Schemes() = %v", got)
	}
}

func TestNewWiresAWSClients(t *testing.T) {
	var loaded []string
	cache := awsclient.NewCacheWithLoader(func(_ context.Context, profile, _ string) (aws.Config, error) {
		loaded = append(loaded, profile)
		return aws.Config{}, errors.New("stop before any network call")
	})
	cfg := &config.Config{AWSProfile: "dev", SSMPrefix: "/app"}
	reg := New(Options{Config: cfg, AWS: cache})

	for _, scheme := range []string{"ssm", "secret"} {
		p, _ := reg.Get(scheme)
		_, err := p.Fetch(context.Background(), []provider.Request{{Path: "/app/x", Profile: "dev"}})
		if err == nil || !strings.Contains(err.Error(), "stop before any network call") {
			t.Errorf("%s: Fetch err = %v, want the cache's loader to be used", scheme, err)
		}
	}
	if len(loaded) != 1 || loaded[0] != "dev" {
		t.Errorf("loader calls = %v, want one shared load for profile dev", loaded)
	}

	// Without a cache the providers are offline.
	p, _ := New(Options{Config: cfg}).Get("ssm")
	if _, err := p.Fetch(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "not configured for remote access") {
		t.Errorf("offline Fetch err = %v", err)
	}
}
