// Package dotenv renders, parses and atomically writes .env files.
//
// Rendering picks the simplest quoting that the common dotenv parsers
// (docker compose, python-dotenv, node dotenv, Vite) all read back as the
// same value:
//
//   - bare when the value only contains safe characters,
//   - single quotes when it contains no single quote or newline (contents
//     are literal everywhere),
//   - double quotes otherwise, escaping \ " $ and newlines.
//
// The $ escape matters: docker compose, POSIX shells and Vite expand $VAR
// and ${VAR} inside double quotes, and all of them read \$ as a literal $.
// python-dotenv and plain node dotenv keep the backslash, so for a value
// with a $ and a ' or newline they see an extra \ before each $.
package dotenv

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Pair is one KEY=value entry.
type Pair struct {
	Key   string
	Value string
}

var (
	bareRe = regexp.MustCompile(`^[A-Za-z0-9_./:@,+=%~-]*$`)
	keyRe  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]*$`)
)

// Quote formats v for the right-hand side of KEY=v.
func Quote(v string) string {
	switch {
	case bareRe.MatchString(v):
		return v
	case !strings.ContainsAny(v, "'\n\r"):
		return "'" + v + "'"
	default:
		r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`, "\n", `\n`, "\r", `\r`)
		return `"` + r.Replace(v) + `"`
	}
}

// Render produces file contents: each header line as a "# " comment, a blank
// line, then the pairs in order.
func Render(header []string, pairs []Pair) []byte {
	var b bytes.Buffer
	for _, h := range header {
		b.WriteString("# ")
		b.WriteString(h)
		b.WriteByte('\n')
	}
	if len(header) > 0 {
		b.WriteByte('\n')
	}
	for _, p := range pairs {
		b.WriteString(p.Key)
		b.WriteByte('=')
		b.WriteString(Quote(p.Value))
		b.WriteByte('\n')
	}
	return b.Bytes()
}

// Parse reads .env content. It understands the output of Render plus the
// common hand-written forms: comments, blank lines, "export " prefixes,
// inline comments after unquoted values, and multi-line quoted values.
func Parse(data []byte) ([]Pair, error) {
	var pairs []Pair
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, rest, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("line %d: expected KEY=value", lineNo)
		}
		key = strings.TrimSpace(key)
		if !keyRe.MatchString(key) {
			return nil, fmt.Errorf("line %d: invalid key %q", lineNo, key)
		}
		rest = strings.TrimLeft(rest, " \t")

		var value string
		switch {
		case strings.HasPrefix(rest, "'"), strings.HasPrefix(rest, `"`):
			q := rest[0]
			body := rest[1:]
			for {
				if end := closingQuote(body, q); end >= 0 {
					body = body[:end]
					break
				}
				if !sc.Scan() {
					return nil, fmt.Errorf("line %d: unterminated %c-quoted value for %s", lineNo, q, key)
				}
				lineNo++
				body += "\n" + sc.Text()
			}
			if q == '"' {
				value = unescape(body)
			} else {
				value = body
			}
		default:
			if i := strings.Index(rest, " #"); i >= 0 {
				rest = rest[:i]
			}
			value = strings.TrimSpace(rest)
		}
		pairs = append(pairs, Pair{Key: key, Value: value})
	}
	return pairs, sc.Err()
}

// closingQuote returns the index of the unescaped closing quote q in s.
func closingQuote(s string, q byte) int {
	for i := 0; i < len(s); i++ {
		if q == '"' && s[i] == '\\' {
			i++
			continue
		}
		if s[i] == q {
			return i
		}
	}
	return -1
}

func unescape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 == len(s) {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch s[i] {
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case '\\', '"', '$':
			b.WriteByte(s[i])
		default:
			b.WriteByte('\\')
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// WriteFile writes data to path atomically with mode 0600, creating parent
// directories. It returns false without touching the file when the existing
// content is identical.
func WriteFile(path string, data []byte) (changed bool, err error) {
	if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, data) {
		return false, nil
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".envhaul-*")
	if err != nil {
		return false, err
	}
	defer func() {
		if err != nil {
			_ = os.Remove(tmp.Name())
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return false, err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return false, err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return false, err
	}
	if err := tmp.Close(); err != nil {
		return false, err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return false, err
	}
	return true, nil
}
