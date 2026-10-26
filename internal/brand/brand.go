// Package brand renders the envhaul banners in the brand colors.
//
// The banner files here are copies of the ones in .github/assets (go:embed
// cannot reach outside the package); a test keeps them in sync.
package brand

import (
	_ "embed"
	"os"
	"strings"
)

// Tagline is the one-line project description used in help, packages and
// docs.
const Tagline = "Haul config and secrets from your vaults into .env files."

var (
	//go:embed banner.txt
	bannerLarge string
	//go:embed banner-ascii.txt
	bannerASCII string
	//go:embed banner-small.txt
	bannerSmall string
)

// banner is a piece of art plus the rune columns of the "env" part of the
// wordmark, which is printed in the default foreground. Everything else (the
// crate, speed lines and "haul") is amber.
type banner struct {
	text             string
	envStart, envEnd int
}

var (
	large = banner{bannerLarge, 23, 50}
	ascii = banner{bannerASCII, 22, 42}
	small = banner{bannerSmall, 11, 14}
)

// Style controls how banners are printed.
type Style struct {
	// Color enables ANSI colors.
	Color bool
	// TrueColor uses 24-bit color instead of the 256-color palette.
	TrueColor bool
	// Unicode allows box-drawing characters; otherwise the ASCII banner is
	// used.
	Unicode bool
}

// DetectStyle derives a Style from the environment. color should already
// reflect --no-color, NO_COLOR and whether the output is a terminal.
func DetectStyle(color bool) Style {
	ct := strings.ToLower(os.Getenv("COLORTERM"))
	return Style{
		Color:     color,
		TrueColor: ct == "truecolor" || ct == "24bit",
		Unicode:   unicodeLocale(),
	}
}

func unicodeLocale() bool {
	for _, k := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if v := os.Getenv(k); v != "" {
			v = strings.ToLower(v)
			return strings.Contains(v, "utf-8") || strings.Contains(v, "utf8")
		}
	}
	// No locale set: modern terminals (including Windows Terminal) handle
	// UTF-8.
	return true
}

// Large returns the full banner for the given style.
func Large(s Style) string {
	if s.Unicode {
		return render(large, s)
	}
	return render(ascii, s)
}

// Small returns the three-line banner. Without Unicode it falls back to the
// plain word "envhaul".
func Small(s Style) string {
	if !s.Unicode {
		return Amber(s, "envhaul") + "\n"
	}
	return render(small, s)
}

// Amber wraps text in the brand amber when colors are enabled.
func Amber(s Style, text string) string {
	if !s.Color || text == "" {
		return text
	}
	return amberOn(s) + text + reset
}

const reset = "\x1b[0m"

func amberOn(s Style) string {
	if s.TrueColor {
		return "\x1b[38;2;251;191;36m" // #FBBF24, amber 400
	}
	return "\x1b[38;5;214m"
}

func render(b banner, s Style) string {
	text := strings.TrimRight(b.text, "\n") + "\n"
	if !s.Color {
		return text
	}
	var out strings.Builder
	for _, line := range strings.SplitAfter(text, "\n") {
		if line == "" {
			continue
		}
		body := strings.TrimSuffix(line, "\n")
		runes := []rune(body)
		var seg []rune
		inAmber := false
		flush := func() {
			if len(seg) == 0 {
				return
			}
			if inAmber {
				out.WriteString(Amber(s, string(seg)))
			} else {
				out.WriteString(string(seg))
			}
			seg = seg[:0]
		}
		for i, r := range runes {
			amber := (i < b.envStart || i >= b.envEnd) && r != ' '
			if r == ' ' {
				amber = inAmber // keep runs of spaces in the current segment
			}
			if amber != inAmber {
				flush()
				inAmber = amber
			}
			seg = append(seg, r)
		}
		flush()
		if strings.HasSuffix(line, "\n") {
			out.WriteByte('\n')
		}
	}
	return out.String()
}
