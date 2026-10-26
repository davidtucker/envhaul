package brand

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestBannersMatchAssets(t *testing.T) {
	for _, name := range []string{"banner.txt", "banner-ascii.txt", "banner-small.txt"} {
		asset, err := os.ReadFile(filepath.Join("..", "..", ".github", "assets", name))
		if err != nil {
			t.Fatal(err)
		}
		embedded, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if string(asset) != string(embedded) {
			t.Errorf("internal/brand/%s is out of date; copy it from .github/assets", name)
		}
	}
}

var ansiRe = regexp.MustCompile("\x1b\\[[0-9;]*m")

func TestColoringPreservesText(t *testing.T) {
	for _, s := range []Style{{Unicode: true}, {}} {
		plain := Large(s)
		colored := Large(Style{Color: true, Unicode: s.Unicode})
		if got := ansiRe.ReplaceAllString(colored, ""); got != plain {
			t.Errorf("stripping colors changed the banner:\n%s\nvs\n%s", got, plain)
		}
	}
	plain := Small(Style{Unicode: true})
	colored := Small(Style{Unicode: true, Color: true, TrueColor: true})
	if ansiRe.ReplaceAllString(colored, "") != plain {
		t.Error("small banner text changed by coloring")
	}
	if !strings.Contains(colored, "\x1b[38;2;251;191;36m") {
		t.Error("truecolor amber not used")
	}
}

func TestEnvIsUncolored(t *testing.T) {
	out := Small(Style{Unicode: true, Color: true})
	// "env" must sit outside any color run, immediately followed by amber "haul".
	if !strings.Contains(out, "\x1b[0menv\x1b[38;5;214mhaul\x1b[0m") {
		t.Errorf("unexpected small banner coloring: %q", out)
	}
}

func TestNoColor(t *testing.T) {
	if strings.Contains(Large(Style{Unicode: true}), "\x1b") {
		t.Error("colors emitted with Color=false")
	}
	if Small(Style{}) != "envhaul\n" {
		t.Errorf("ASCII small banner = %q", Small(Style{}))
	}
}

func TestDetectStyle(t *testing.T) {
	clear := func() {
		for _, k := range []string{"COLORTERM", "LC_ALL", "LC_CTYPE", "LANG"} {
			t.Setenv(k, "")
		}
	}
	tests := []struct {
		name string
		env  map[string]string
		want Style
	}{
		{"no locale defaults to unicode", nil, Style{Color: true, Unicode: true}},
		{"truecolor", map[string]string{"COLORTERM": "truecolor"}, Style{Color: true, TrueColor: true, Unicode: true}},
		{"24bit", map[string]string{"COLORTERM": "24bit"}, Style{Color: true, TrueColor: true, Unicode: true}},
		{"utf-8 LANG", map[string]string{"LANG": "en_US.UTF-8"}, Style{Color: true, Unicode: true}},
		{"C locale is ascii", map[string]string{"LANG": "C"}, Style{Color: true}},
		{"LC_ALL wins over LANG", map[string]string{"LC_ALL": "POSIX", "LANG": "en_US.UTF-8"}, Style{Color: true}},
		{"utf8 spelling", map[string]string{"LC_CTYPE": "en_GB.utf8"}, Style{Color: true, Unicode: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clear()
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			if got := DetectStyle(true); got != tt.want {
				t.Errorf("DetectStyle = %+v, want %+v", got, tt.want)
			}
		})
	}
	clear()
	if DetectStyle(false).Color {
		t.Error("color=false must disable color")
	}
}
