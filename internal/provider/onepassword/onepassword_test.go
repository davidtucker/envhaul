package onepassword

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/davidtucker/envhaul/internal/config"
	"github.com/davidtucker/envhaul/internal/provider"
	"github.com/davidtucker/envhaul/internal/template"
)

// fakeOp answers `op read` from items keyed by "account|op://vault/item/field".
type fakeOp struct {
	mu    sync.Mutex
	items map[string]string
	calls [][]string
	fail  map[string]string // account -> stderr
}

func (f *fakeOp) run(_ context.Context, args []string) ([]byte, []byte, error) {
	f.mu.Lock()
	f.calls = append(f.calls, args)
	f.mu.Unlock()
	if args[0] != "read" || args[1] != "--no-newline" {
		return nil, []byte("unexpected args"), errors.New("exit status 1")
	}
	acct := ""
	if i := slices.Index(args, "--account"); i >= 0 {
		acct = args[i+1]
	}
	if stderr, ok := f.fail[acct]; ok {
		return nil, []byte(stderr), errors.New("exit status 1")
	}
	ref := args[len(args)-1]
	if v, ok := f.items[acct+"|"+ref]; ok {
		return []byte(v), nil, nil
	}
	return nil, []byte(fmt.Sprintf(`[ERROR] 2026/10/26 09:00:00 "%s" isn't an item in the "Private" vault. Specify the item with its UUID, name, or domain.`, ref)), errors.New("exit status 1")
}

func ref(t *testing.T, s string) *template.Ref {
	t.Helper()
	tmpl, err := template.Parse("{{" + s + "}}")
	if err != nil {
		t.Fatal(err)
	}
	return tmpl.Refs()[0]
}

func TestValidateAndRequest(t *testing.T) {
	p := New(Options{})
	for _, ok := range []string{"op:Dev/db/password", "op:Dev/db/admin/password", "op:op://Dev/db/password"} {
		if err := p.Validate(ref(t, ok)); err != nil {
			t.Errorf("Validate(%s): %v", ok, err)
		}
	}
	for _, bad := range []string{"op:Dev/db", "op:a/b/c/d/e", "op:Dev//password"} {
		if err := p.Validate(ref(t, bad)); err == nil {
			t.Errorf("Validate(%s) should fail", bad)
		}
	}
	if got := p.Request(ref(t, "op:op://Dev/db/password@work")); got != (provider.Request{Path: "Dev/db/password", Profile: "work"}) {
		t.Errorf("Request = %+v", got)
	}
	if p.Request(ref(t, "op:Dev/db/password")) != p.Request(ref(t, "op:op://Dev/db/password")) {
		t.Error("the op:// prefix should not create a different request")
	}
	if p.Scheme() != "op" || p.Description() != "1Password" {
		t.Error("identity")
	}
}

func TestFetch(t *testing.T) {
	fake := &fakeOp{items: map[string]string{
		"team.1password.com|op://Dev/db/password": "s3cret\n with newline",
		"team.1password.com|op://Dev/api/key":     "k-1",
		"work.1password.com|op://Ops/token/value": "w-1",
		"personal|op://Private/x/y":               "p-1",
	}}
	src := &config.Source{
		Settings: map[string]string{"account": "team.1password.com"},
		Profiles: map[string]map[string]string{"work": {"account": "work.1password.com"}},
	}
	p := New(Options{Source: src, Online: true, Run: fake.run})
	reqs := []provider.Request{
		p.Request(ref(t, "op:Dev/db/password")),
		p.Request(ref(t, "op:Dev/api/key")),
		p.Request(ref(t, "op:Ops/token/value@work")),
		p.Request(ref(t, "op:Private/x/y@personal")),
		p.Request(ref(t, "op:Dev/missing/field")),
	}
	got, err := p.Fetch(context.Background(), reqs)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"s3cret\n with newline", "k-1", "w-1", "p-1"}
	for i, w := range want {
		if r := got[reqs[i]]; !r.Found || r.Value != w {
			t.Errorf("%s = %+v, want %q", reqs[i].Path, r, w)
		}
	}
	if got[reqs[4]].Found {
		t.Error("a missing item should be reported as not found")
	}
	// The first call per account runs before any concurrent ones.
	accounts := map[string]int{}
	for i, c := range fake.calls {
		acct := c[slices.Index(c, "--account")+1]
		if _, seen := accounts[acct]; !seen {
			accounts[acct] = i
		}
	}
	if len(accounts) != 3 {
		t.Errorf("accounts used = %v", accounts)
	}
}

func TestDefaultAccountOmitsFlag(t *testing.T) {
	fake := &fakeOp{items: map[string]string{"|op://Dev/db/password": "x"}}
	p := New(Options{Online: true, Run: fake.run})
	r := p.Request(ref(t, "op:Dev/db/password"))
	got, err := p.Fetch(context.Background(), []provider.Request{r})
	if err != nil || got[r].Value != "x" {
		t.Fatalf("got %v, %v", got, err)
	}
	if slices.Contains(fake.calls[0], "--account") {
		t.Errorf("no account configured, but op was called with %v", fake.calls[0])
	}
}

func TestFetchErrors(t *testing.T) {
	r := provider.Request{Path: "Dev/db/password"}
	tests := []struct {
		name string
		run  Runner
		want string
	}{
		{"op missing", func(context.Context, []string) ([]byte, []byte, error) {
			return nil, nil, &exec.Error{Name: "op", Err: exec.ErrNotFound}
		}, "1Password CLI (op) not found on PATH"},
		{
			"not signed in", (&fakeOp{fail: map[string]string{"": "[ERROR] 2026/10/26 09:00:00 You are not currently signed in. Please run `op signin --help` for instructions"}}).run,
			"not signed in to the default account",
		},
		{
			"other error", (&fakeOp{fail: map[string]string{"": "[ERROR] 2026/10/26 09:00:00 rate limit exceeded"}}).run,
			"reading op://Dev/db/password: rate limit exceeded",
		},
		{"no stderr", func(context.Context, []string) ([]byte, []byte, error) {
			return nil, nil, errors.New("signal: killed")
		}, "signal: killed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(Options{Online: true, Run: tt.run}).Fetch(context.Background(), []provider.Request{r})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want %q", err, tt.want)
			}
		})
	}

	src := &config.Source{Settings: map[string]string{"account": "team"}}
	fake := &fakeOp{fail: map[string]string{"team": "account is not signed in"}}
	_, err := New(Options{Source: src, Online: true, Run: fake.run}).Fetch(context.Background(), []provider.Request{r})
	if err == nil || !strings.Contains(err.Error(), `not signed in to account "team"`) {
		t.Errorf("named account err = %v", err)
	}

	if _, err := New(Options{}).Fetch(context.Background(), nil); err == nil {
		t.Error("offline provider should refuse to fetch")
	}
}

func TestConcurrentReadsAfterFirst(t *testing.T) {
	fake := &fakeOp{items: map[string]string{}}
	var reqs []provider.Request
	for i := range 10 {
		path := fmt.Sprintf("Dev/item%d/field", i)
		fake.items["|op://"+path] = fmt.Sprint(i)
		reqs = append(reqs, provider.Request{Path: path})
	}
	got, err := New(Options{Online: true, Run: fake.run}).Fetch(context.Background(), reqs)
	if err != nil {
		t.Fatal(err)
	}
	for i, r := range reqs {
		if got[r].Value != fmt.Sprint(i) {
			t.Errorf("%s = %+v", r.Path, got[r])
		}
	}
	if len(fake.calls) != 10 {
		t.Errorf("op called %d times, want 10", len(fake.calls))
	}
}

func TestSpacesInNames(t *testing.T) {
	fake := &fakeOp{items: map[string]string{"|op://Ops/Deploy key/private key": "k"}}
	p := New(Options{Online: true, Run: fake.run})
	r := ref(t, "op:Ops/Deploy key/private key")
	if err := p.Validate(r); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	req := p.Request(r)
	got, err := p.Fetch(context.Background(), []provider.Request{req})
	if err != nil || got[req].Value != "k" {
		t.Errorf("got %v, %v", got, err)
	}
	if args := fake.calls[0]; args[len(args)-1] != "op://Ops/Deploy key/private key" {
		t.Errorf("op got %q as one argument? args=%q", args[len(args)-1], args)
	}
}
