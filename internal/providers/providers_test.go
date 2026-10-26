package providers

import (
	"testing"

	"github.com/davidtucker/envhaul/internal/awsclient"
	"github.com/davidtucker/envhaul/internal/config"
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
