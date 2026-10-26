// Package config loads and validates envhaul configuration files.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/davidtucker/envhaul/internal/pipe"
	"github.com/davidtucker/envhaul/internal/template"
)

// CurrentVersion is the newest config schema version this build understands.
const CurrentVersion = 1

// Config is a parsed envhaul configuration file.
type Config struct {
	// Path is the absolute path of the file the config was loaded from.
	Path string

	Version      int
	Name         string
	Description  string
	AWSProfile   string
	AWSRegion    string
	SSMPrefix    string
	SecretPrefix string

	// Components are in file order.
	Components []*Component
}

// Component is one generated env file.
type Component struct {
	Name        string
	Description string
	// EnvFile is the path as written in the config.
	EnvFile string
	// Vars are in file order.
	Vars []*Var
	Pos  Pos
}

// Var is a single variable and its parsed template.
type Var struct {
	Name     string
	Raw      string
	Template *template.Template
	Pos      Pos
}

// Pos is a 1-based line and column in the config file.
type Pos struct {
	Line, Col int
}

// Diagnostic is a problem found while loading a config file.
type Diagnostic struct {
	File string
	Pos  Pos
	Msg  string
}

func (d Diagnostic) String() string {
	if d.Pos.Line > 0 {
		return fmt.Sprintf("%s:%d:%d: %s", d.File, d.Pos.Line, d.Pos.Col, d.Msg)
	}
	return fmt.Sprintf("%s: %s", d.File, d.Msg)
}

// Diagnostics is returned by Load when the file is invalid. It lists every
// problem found, not just the first.
type Diagnostics []Diagnostic

func (ds Diagnostics) Error() string {
	lines := make([]string, len(ds))
	for i, d := range ds {
		lines[i] = d.String()
	}
	return strings.Join(lines, "\n")
}

// Options customize Load.
type Options struct {
	// ValidateRef, if set, performs provider-specific checks on each
	// reference (for example, that its scheme is known).
	ValidateRef func(*template.Ref) error
}

// TopLevelKeys are the keys allowed at the top of a config file. It is used
// by tests to keep the published JSON Schema in sync.
var TopLevelKeys = []string{
	"version", "name", "description", "aws_profile", "aws_region", "ssm_prefix", "secret_prefix", "components",
}

// ComponentKeys are the keys allowed in a component.
var ComponentKeys = []string{"description", "env_file", "vars"}

var varNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Load reads and validates the config file at path.
func Load(path string, opts Options) (*Config, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	return Parse(abs, data, opts)
}

// Parse validates config data. path is used for diagnostics and to resolve
// relative env_file paths; it should be absolute.
func Parse(path string, data []byte, opts Options) (*Config, error) {
	p := &parser{file: displayPath(path), opts: opts}
	cfg := &Config{Path: path, Version: CurrentVersion}

	var doc yaml.Node
	dec := yaml.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, Diagnostics{{File: p.file, Msg: "config file is empty"}}
		}
		return nil, Diagnostics{{File: p.file, Msg: "invalid YAML: " + strings.TrimPrefix(err.Error(), "yaml: ")}}
	}
	if len(doc.Content) == 0 {
		return nil, Diagnostics{{File: p.file, Msg: "config file is empty"}}
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		p.errorf(root, "top level must be a mapping")
		return nil, p.diags
	}

	sawComponents := false
	p.eachPair(root, TopLevelKeys, func(key string, k, v *yaml.Node) {
		switch key {
		case "version":
			s, ok := p.scalar(v, key)
			if !ok {
				return
			}
			n, err := strconv.Atoi(s)
			if err != nil || n < 1 {
				p.errorf(v, "version must be a positive integer")
				return
			}
			if n > CurrentVersion {
				p.errorf(v, "config version %d is newer than this envhaul supports (max %d); upgrade envhaul", n, CurrentVersion)
				return
			}
			cfg.Version = n
		case "name":
			cfg.Name, _ = p.scalar(v, key)
		case "description":
			cfg.Description, _ = p.scalar(v, key)
		case "aws_profile":
			cfg.AWSProfile, _ = p.scalar(v, key)
		case "aws_region":
			cfg.AWSRegion, _ = p.scalar(v, key)
		case "ssm_prefix":
			cfg.SSMPrefix, _ = p.scalar(v, key)
		case "secret_prefix":
			cfg.SecretPrefix, _ = p.scalar(v, key)
		case "components":
			sawComponents = true
			cfg.Components = p.components(v)
		}
	})
	if !sawComponents {
		p.errorf(root, "missing required key %q", "components")
	}
	if cfg.SSMPrefix != "" && !strings.HasPrefix(cfg.SSMPrefix, "/") {
		p.diags = append(p.diags, Diagnostic{File: p.file, Msg: fmt.Sprintf("ssm_prefix %q must start with /", cfg.SSMPrefix)})
	}

	// Two components writing the same file would silently clobber each other.
	seen := map[string]*Component{}
	for _, c := range cfg.Components {
		if c.EnvFile == "" {
			continue
		}
		target := cfg.envFilePath(c.EnvFile)
		if prev, ok := seen[target]; ok {
			p.diags = append(p.diags, Diagnostic{
				File: p.file, Pos: c.Pos,
				Msg: fmt.Sprintf("component %q writes the same env_file as %q (%s)", c.Name, prev.Name, c.EnvFile),
			})
			continue
		}
		seen[target] = c
	}

	if len(p.diags) > 0 {
		return nil, p.diags
	}
	return cfg, nil
}

func (p *parser) components(n *yaml.Node) []*Component {
	if n.Kind != yaml.MappingNode {
		p.errorf(n, "components must be a mapping of component name to settings")
		return nil
	}
	if len(n.Content) == 0 {
		p.errorf(n, "components must define at least one component")
	}
	var out []*Component
	p.eachPair(n, nil, func(name string, k, v *yaml.Node) {
		c := &Component{Name: name, Pos: pos(k)}
		if strings.ContainsAny(name, " \t/\\") || name == "" {
			p.errorf(k, "invalid component name %q (no spaces or slashes)", name)
		}
		if v.Kind != yaml.MappingNode {
			p.errorf(v, "component %q must be a mapping with env_file and vars", name)
			return
		}
		sawVars := false
		p.eachPair(v, ComponentKeys, func(key string, _, cv *yaml.Node) {
			switch key {
			case "description":
				c.Description, _ = p.scalar(cv, key)
			case "env_file":
				c.EnvFile, _ = p.scalar(cv, key)
				if c.EnvFile == "" {
					p.errorf(cv, "env_file must not be empty")
				}
			case "vars":
				sawVars = true
				c.Vars = p.vars(cv)
			}
		})
		if c.EnvFile == "" {
			p.errorf(k, "component %q is missing env_file", name)
		}
		if !sawVars {
			p.errorf(k, "component %q is missing vars", name)
		}
		out = append(out, c)
	})
	return out
}

func (p *parser) vars(n *yaml.Node) []*Var {
	if n.Kind != yaml.MappingNode {
		if isNull(n) {
			return nil
		}
		p.errorf(n, "vars must be a mapping of NAME: value")
		return nil
	}
	var out []*Var
	p.eachPair(n, nil, func(name string, k, v *yaml.Node) {
		if !varNameRe.MatchString(name) {
			p.errorf(k, "invalid variable name %q (must match [A-Za-z_][A-Za-z0-9_]*)", name)
			return
		}
		raw, ok := p.scalar(v, name)
		if !ok {
			return
		}
		tmpl, err := template.Parse(raw)
		if err != nil {
			p.errorAt(v, raw, err)
			return
		}
		for _, ref := range tmpl.Refs() {
			for _, c := range ref.Pipes {
				if err := pipe.Validate(c); err != nil {
					p.errorAtOffset(v, raw, ref.Offset, err.Error())
				}
			}
			if p.opts.ValidateRef != nil {
				if err := p.opts.ValidateRef(ref); err != nil {
					p.errorAtOffset(v, raw, ref.Offset, err.Error())
				}
			}
		}
		out = append(out, &Var{Name: name, Raw: raw, Template: tmpl, Pos: pos(v)})
	})
	return out
}

type parser struct {
	file  string
	opts  Options
	diags Diagnostics
}

func (p *parser) errorf(n *yaml.Node, format string, args ...any) {
	p.diags = append(p.diags, Diagnostic{File: p.file, Pos: pos(n), Msg: fmt.Sprintf(format, args...)})
}

// errorAt reports a template parse error, pointing at the offending column
// when the scalar is on a single line.
func (p *parser) errorAt(n *yaml.Node, raw string, err error) {
	offset := 0
	var te *template.Error
	if errors.As(err, &te) {
		offset = te.Offset
	}
	p.errorAtOffset(n, raw, offset, err.Error())
}

func (p *parser) errorAtOffset(n *yaml.Node, raw string, offset int, msg string) {
	at := pos(n)
	if n.Style == 0 && !strings.Contains(raw, "\n") {
		at.Col += offset
	} else if (n.Style == yaml.DoubleQuotedStyle || n.Style == yaml.SingleQuotedStyle) && !strings.Contains(raw, "\n") {
		at.Col += offset + 1
	}
	p.diags = append(p.diags, Diagnostic{File: p.file, Pos: at, Msg: msg})
}

// eachPair iterates a mapping node in order, rejecting duplicate keys and,
// when allowed is non-nil, unknown keys.
func (p *parser) eachPair(n *yaml.Node, allowed []string, fn func(key string, k, v *yaml.Node)) {
	seen := map[string]bool{}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if k.Kind != yaml.ScalarNode {
			p.errorf(k, "keys must be strings")
			continue
		}
		key := k.Value
		if seen[key] {
			p.errorf(k, "duplicate key %q", key)
			continue
		}
		seen[key] = true
		if allowed != nil && !contains(allowed, key) {
			p.errorf(k, "unknown key %q (allowed: %s)", key, strings.Join(sorted(allowed), ", "))
			continue
		}
		fn(key, k, v)
	}
}

// scalar returns the literal text of a scalar node. YAML type resolution is
// deliberately ignored so that values like 1.10, 0755, yes and 1e3 are kept
// exactly as written; null becomes the empty string.
func (p *parser) scalar(n *yaml.Node, key string) (string, bool) {
	if n.Kind == yaml.AliasNode && n.Alias != nil {
		n = n.Alias
	}
	if n.Kind != yaml.ScalarNode {
		p.errorf(n, "%s must be a string, number or boolean, not a %s", key, kindName(n.Kind))
		return "", false
	}
	if isNull(n) {
		return "", true
	}
	return n.Value, true
}

func isNull(n *yaml.Node) bool {
	return n.Kind == yaml.ScalarNode && n.Tag == "!!null" && n.Style == 0
}

func kindName(k yaml.Kind) string {
	switch k {
	case yaml.MappingNode:
		return "mapping"
	case yaml.SequenceNode:
		return "list"
	default:
		return "complex value"
	}
}

func pos(n *yaml.Node) Pos { return Pos{Line: n.Line, Col: n.Column} }

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func sorted(list []string) []string {
	out := append([]string(nil), list...)
	sort.Strings(out)
	return out
}

// displayPath shortens path relative to the working directory when possible.
func displayPath(path string) string {
	wd, err := os.Getwd()
	if err != nil {
		return path
	}
	if rel, err := filepath.Rel(wd, path); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return path
}

// Dir is the directory relative env_file paths are resolved against.
func (c *Config) Dir() string { return filepath.Dir(c.Path) }

func (c *Config) envFilePath(p string) string {
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Join(c.Dir(), filepath.FromSlash(p))
}

// EnvFilePath returns the absolute path a component's env file is written to.
func (c *Config) EnvFilePath(comp *Component) string { return c.envFilePath(comp.EnvFile) }

// Component returns the named component, or nil.
func (c *Config) Component(name string) *Component {
	for _, comp := range c.Components {
		if comp.Name == name {
			return comp
		}
	}
	return nil
}

// ComponentNames returns component names in file order.
func (c *Config) ComponentNames() []string {
	names := make([]string, len(c.Components))
	for i, comp := range c.Components {
		names[i] = comp.Name
	}
	return names
}

// Select returns the named components in file order, or all components when
// names is empty.
func (c *Config) Select(names []string) ([]*Component, error) {
	if len(names) == 0 {
		return c.Components, nil
	}
	want := map[string]bool{}
	for _, n := range names {
		if c.Component(n) == nil {
			return nil, fmt.Errorf("unknown component %q (available: %s)", n, strings.Join(c.ComponentNames(), ", "))
		}
		want[n] = true
	}
	var out []*Component
	for _, comp := range c.Components {
		if want[comp.Name] {
			out = append(out, comp)
		}
	}
	return out, nil
}

// Var returns the named variable, or nil.
func (c *Component) Var(name string) *Var {
	for _, v := range c.Vars {
		if v.Name == name {
			return v
		}
	}
	return nil
}
