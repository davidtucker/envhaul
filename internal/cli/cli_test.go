package cli

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	smtypes "github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
	"github.com/rogpeppe/go-internal/testscript"

	"github.com/davidtucker/envhaul/internal/config"
	"github.com/davidtucker/envhaul/internal/provider"
	"github.com/davidtucker/envhaul/internal/provider/awssecrets"
	"github.com/davidtucker/envhaul/internal/provider/awsssm"
	"github.com/davidtucker/envhaul/internal/providers"
)

// The scripts in testdata/script run the real CLI and real providers against
// fake AWS clients. Values come from fake-aws.json in the script's working
// directory:
//
//	{"ssm": {"profile|/name": "value"}, "secret": {"profile|id": "value"},
//	 "errors": {"profile": "message"}}
//
// An empty profile is written as "default".
type fakeAWS struct {
	SSM    map[string]string `json:"ssm"`
	Secret map[string]string `json:"secret"`
	Errors map[string]string `json:"errors"`
}

func loadFake() *fakeAWS {
	f := &fakeAWS{}
	if data, err := os.ReadFile("fake-aws.json"); err == nil {
		if err := json.Unmarshal(data, f); err != nil {
			panic(err)
		}
	}
	return f
}

func profileKey(p string) string {
	if p == "" {
		return "default"
	}
	return p
}

type fakeSSM struct {
	f       *fakeAWS
	profile string
}

func (c fakeSSM) GetParameters(_ context.Context, in *ssm.GetParametersInput, _ ...func(*ssm.Options)) (*ssm.GetParametersOutput, error) {
	if msg, ok := c.f.Errors[c.profile]; ok {
		return nil, errors.New(msg)
	}
	out := &ssm.GetParametersOutput{}
	for _, n := range in.Names {
		if v, ok := c.f.SSM[c.profile+"|"+n]; ok {
			out.Parameters = append(out.Parameters, ssmtypes.Parameter{Name: aws.String(n), Value: aws.String(v)})
		} else {
			out.InvalidParameters = append(out.InvalidParameters, n)
		}
	}
	return out, nil
}

type fakeSM struct {
	f       *fakeAWS
	profile string
}

func (c fakeSM) GetSecretValue(_ context.Context, in *secretsmanager.GetSecretValueInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
	if msg, ok := c.f.Errors[c.profile]; ok {
		return nil, errors.New(msg)
	}
	if v, ok := c.f.Secret[c.profile+"|"+aws.ToString(in.SecretId)]; ok {
		return &secretsmanager.GetSecretValueOutput{SecretString: aws.String(v)}, nil
	}
	return nil, &smtypes.ResourceNotFoundException{Message: aws.String("not found")}
}

func fakeRegistry(cfg *config.Config, g *Globals, logger *slog.Logger) *provider.Registry {
	f := loadFake()
	settings := providers.AWSSettings(cfg, g.Profile, g.ProfileSet, g.Region)
	return provider.NewRegistry(
		awsssm.New(awsssm.Options{
			Prefix: cfg.SSMPrefix, Settings: settings, Logger: logger,
			Clients: func(_ context.Context, profile, _ string) (awsssm.API, error) {
				return fakeSSM{f, profileKey(profile)}, nil
			},
		}),
		awssecrets.New(awssecrets.Options{
			Prefix: cfg.SecretPrefix, Settings: settings, Logger: logger,
			Clients: func(_ context.Context, profile, _ string) (awssecrets.API, error) {
				return fakeSM{f, profileKey(profile)}, nil
			},
		}),
	)
}

func TestMain(m *testing.M) {
	testscript.Main(m, map[string]func(){
		"envhaul": func() {
			os.Exit(Run(os.Args[1:], Options{NewRegistry: fakeRegistry}))
		},
	})
}

func TestScripts(t *testing.T) {
	testscript.Run(t, testscript.Params{
		Dir: "testdata/script",
		Setup: func(env *testscript.Env) error {
			env.Setenv("NO_COLOR", "1")
			env.Setenv("ENVHAUL_CONFIG", "")
			return nil
		},
		Cmds: map[string]func(ts *testscript.TestScript, neg bool, args []string){
			// mode FILE PERM asserts a file's permission bits (octal).
			"mode": func(ts *testscript.TestScript, neg bool, args []string) {
				if len(args) != 2 {
					ts.Fatalf("usage: mode file perm")
				}
				info, err := os.Stat(ts.MkAbs(args[0]))
				ts.Check(err)
				got := info.Mode().Perm()
				want := args[1]
				if ok := strings.TrimLeft(want, "0") == strings.TrimLeft(octal(got), "0"); ok == neg {
					ts.Fatalf("mode of %s is %s, want %s", args[0], octal(got), want)
				}
			},
		},
	})
}

func octal(m os.FileMode) string {
	const digits = "01234567"
	v := uint32(m)
	out := ""
	for i := 0; i < 3; i++ {
		out = string(digits[v&7]) + out
		v >>= 3
	}
	return "0" + out
}

func TestSetEnv(t *testing.T) {
	env := setEnv([]string{"A=1", "B=2", "AB=3"}, "A", "x")
	if strings.Join(env, ",") != "B=2,AB=3,A=x" {
		t.Errorf("setEnv = %v", env)
	}
}

func TestSanitizeName(t *testing.T) {
	for in, want := range map[string]string{"My App": "my-app", "envhaul": "envhaul", "***": "myapp"} {
		if got := sanitizeName(in); got != want {
			t.Errorf("sanitizeName(%q) = %q, want %q", in, got, want)
		}
	}
}
