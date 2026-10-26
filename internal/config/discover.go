package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// FileNames are the config file names searched for, in order of preference.
var FileNames = []string{"envhaul.yaml", "envhaul.yml", ".envhaul.yaml", ".envhaul.yml"}

// ErrNotFound is returned by Discover when no config file exists.
var ErrNotFound = errors.New("no envhaul config file found")

// Discover searches start and its parents for a config file, stopping at the
// root of the enclosing git repository (the first directory containing .git)
// or the filesystem root.
func Discover(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		for _, name := range FileNames {
			p := filepath.Join(dir, name)
			if info, err := os.Stat(p); err == nil && !info.IsDir() {
				return p, nil
			}
		}
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("%w (looked for %s from %s up to the repository root; use --config to point at one, or run `envhaul init`)",
		ErrNotFound, FileNames[0], start)
}
