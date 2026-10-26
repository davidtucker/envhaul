package template

import (
	"reflect"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		parts []Part
	}{
		{name: "empty", in: "", parts: nil},
		{name: "literal", in: "http://localhost:5173", parts: []Part{{Literal: "http://localhost:5173"}}},
		{
			name:  "ssm with profile",
			in:    "{{ssm:api/OPENAI_API_KEY@acme-shared}}",
			parts: []Part{{Ref: &Ref{Scheme: "ssm", Path: "api/OPENAI_API_KEY", Profile: "acme-shared"}}},
		},
		{
			name:  "secret with key and profile",
			in:    "{{secret:shared/api-key#key@acme-shared}}",
			parts: []Part{{Ref: &Ref{Scheme: "secret", Path: "shared/api-key", Key: "key", Profile: "acme-shared"}}},
		},
		{
			name: "interpolation",
			in:   "postgresql://{{secret:db#user}}:{{secret:db#pass|urlencode}}@host:5432/db",
			parts: []Part{
				{Literal: "postgresql://"},
				{Ref: &Ref{Scheme: "secret", Path: "db", Key: "user"}},
				{Literal: ":"},
				{Ref: &Ref{Scheme: "secret", Path: "db", Key: "pass", Pipes: []PipeCall{{Name: "urlencode"}}}},
				{Literal: "@host:5432/db"},
			},
		},
		{
			name: "pipe chain and default with colon",
			in:   "{{ ssm:optional | trim | default:http://localhost:8000 }}",
			parts: []Part{{Ref: &Ref{Scheme: "ssm", Path: "optional", Pipes: []PipeCall{
				{Name: "trim"},
				{Name: "default", Arg: "http://localhost:8000", HasArg: true},
			}}}},
		},
		{
			name:  "default with empty arg",
			in:    "{{ssm:x|default:}}",
			parts: []Part{{Ref: &Ref{Scheme: "ssm", Path: "x", Pipes: []PipeCall{{Name: "default", HasArg: true}}}}},
		},
		{
			name:  "at sign in secret name with explicit profile",
			in:    "{{secret:team@corp/key@prod}}",
			parts: []Part{{Ref: &Ref{Scheme: "secret", Path: "team@corp/key", Profile: "prod"}}},
		},
		{
			name:  "at sign in secret name with trailing at for default profile",
			in:    "{{secret:team@corp/key@}}",
			parts: []Part{{Ref: &Ref{Scheme: "secret", Path: "team@corp/key"}}},
		},
		{
			name:  "absolute ssm path with version selector",
			in:    "{{ssm:/app/prod/db:3}}",
			parts: []Part{{Ref: &Ref{Scheme: "ssm", Path: "/app/prod/db:3"}}},
		},
		{
			name:  "escaped braces",
			in:    `literal \{{ssm:x}} here`,
			parts: []Part{{Literal: "literal {{ssm:x}} here"}},
		},
		{
			name:  "lone braces are literal",
			in:    "a { b } c }}",
			parts: []Part{{Literal: "a { b } c }}"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.in)
			if err != nil {
				t.Fatalf("Parse(%q) error: %v", tt.in, err)
			}
			for _, p := range got.Parts {
				if p.Ref != nil {
					p.Ref.Raw, p.Ref.Offset = "", 0
				}
			}
			if !reflect.DeepEqual(got.Parts, tt.parts) {
				t.Errorf("Parse(%q)\n got: %#v\nwant: %#v", tt.in, got.Parts, tt.parts)
			}
		})
	}
}

func TestParseRawAndOffset(t *testing.T) {
	tmpl, err := Parse("x={{ssm:a}} y={{ secret:b#k }}")
	if err != nil {
		t.Fatal(err)
	}
	refs := tmpl.Refs()
	if len(refs) != 2 {
		t.Fatalf("got %d refs, want 2", len(refs))
	}
	if refs[0].Raw != "{{ssm:a}}" || refs[0].Offset != 2 {
		t.Errorf("ref 0: raw=%q offset=%d", refs[0].Raw, refs[0].Offset)
	}
	if refs[1].Raw != "{{ secret:b#k }}" || refs[1].Offset != 14 {
		t.Errorf("ref 1: raw=%q offset=%d", refs[1].Raw, refs[1].Offset)
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"{{ssm:x", "unterminated"},
		{"{{}}", "empty reference"},
		{"{{ }}", "empty reference"},
		{"{{noscheme}}", "missing a scheme"},
		{"{{SSM:x}}", "invalid scheme"},
		{"{{ssm:}}", "empty path"},
		{"{{ssm:#k}}", "empty path"},
		{"{{secret:x#}}", "empty key"},
		{"{{ssm:a b}}", "whitespace"},
		{"{{ssm:x@bad profile}}", "invalid profile"},
		{"{{ssm:x|}}", "invalid pipe"},
		{"{{ssm:x|Upper}}", "invalid pipe"},
		{"{{ssm:{{x}}", "nested"},
	}
	for _, tt := range tests {
		_, err := Parse(tt.in)
		if err == nil {
			t.Errorf("Parse(%q): expected error containing %q", tt.in, tt.want)
			continue
		}
		if !strings.Contains(err.Error(), tt.want) {
			t.Errorf("Parse(%q) error = %q, want it to contain %q", tt.in, err, tt.want)
		}
	}
}

func TestRefStringAtInPath(t *testing.T) {
	// An "@" in the path or key with no profile needs a trailing "@" so the
	// canonical form doesn't read the path's "@" as the profile separator.
	for _, in := range []string{"{{secret:team@corp/key@}}", "{{a:@@}}", "{{a:0000#000@@}}"} {
		tmpl, err := Parse(in)
		if err != nil {
			t.Fatal(err)
		}
		ref := tmpl.Refs()[0]
		again, err := Parse("{{" + ref.String() + "}}")
		if err != nil {
			t.Fatalf("canonical form %q of %q: %v", ref.String(), in, err)
		}
		if got := again.Refs()[0]; got.Path != ref.Path || got.Profile != "" {
			t.Errorf("%q round-tripped to path %q profile %q", in, got.Path, got.Profile)
		}
	}
}

func TestRefString(t *testing.T) {
	in := "{{ secret:db#pass@prod | trim | default:x:y }}"
	tmpl, err := Parse(in)
	if err != nil {
		t.Fatal(err)
	}
	got := tmpl.Refs()[0].String()
	want := "secret:db#pass@prod|trim|default:x:y"
	if got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
	// The canonical form must re-parse to the same reference.
	again, err := Parse("{{" + got + "}}")
	if err != nil {
		t.Fatal(err)
	}
	if again.Refs()[0].String() != want {
		t.Errorf("round trip mismatch: %q", again.Refs()[0].String())
	}
}

func TestLiteral(t *testing.T) {
	tmpl, err := Parse(`a\{{b`)
	if err != nil {
		t.Fatal(err)
	}
	if !tmpl.IsLiteral() || tmpl.Literal() != "a{{b" {
		t.Errorf("IsLiteral=%v Literal=%q", tmpl.IsLiteral(), tmpl.Literal())
	}
}

func FuzzParse(f *testing.F) {
	for _, s := range []string{
		"", "plain", "{{ssm:a}}", "x{{secret:b#k@p|trim|default:z}}y", `\{{`, "{{", "}}", "{{ssm:a|b:c|d}}",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		tmpl, err := Parse(s)
		if err != nil {
			return
		}
		// Every reference must re-parse from its canonical form.
		for _, r := range tmpl.Refs() {
			again, err := Parse("{{" + r.String() + "}}")
			if err != nil {
				t.Fatalf("canonical form %q of %q failed to parse: %v", r.String(), r.Raw, err)
			}
			if got := again.Refs()[0].String(); got != r.String() {
				t.Fatalf("canonical form not stable: %q -> %q", r.String(), got)
			}
		}
	})
}
