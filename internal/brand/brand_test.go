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
