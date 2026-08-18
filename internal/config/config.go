// Package config reads the per-project habitat.yml.
//
// It exists so a project states once where its suites live and how to launch
// its runner, rather than repeating both on every command line.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Filename is the config file looked for in the working directory.
const Filename = "habitat.yml"

// Defaults applied when the file is absent or a field is unset.
const (
	DefaultSuites = "evals/suites"
	DefaultDB     = ".habitat/habitat.db"
)

// Config is the project's habitat settings.
type Config struct {
	// Suites is the directory scanned for suite files.
	Suites string `yaml:"suites"`
	// Runner is the command that executes a run, e.g.
	// ["bundle", "exec", "ruby", "evals/runner.rb"]. The SDK inside that
	// process does the fetching and posting; habitat only launches it.
	Runner []string `yaml:"runner"`
	// DB is the SQLite file runs persist to.
	DB string `yaml:"db"`
	// Dir is the directory the config was found in. Not part of the format.
	Dir string `yaml:"-"`
}

// Load reads habitat.yml from dir, falling back to defaults when it is
// absent. A malformed file is an error: silently defaulting would hide a typo
// in the one file that says how to run anything.
func Load(dir string) (Config, error) {
	cfg := Config{Dir: dir}
	path := filepath.Join(dir, Filename)

	data, err := os.ReadFile(path) // #nosec G304 -- operator's own project file
	switch {
	case errors.Is(err, os.ErrNotExist):
		return cfg.withDefaults(), nil
	case err != nil:
		return cfg, err
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("%s: %w", Filename, err)
	}
	cfg.Dir = dir
	return cfg.withDefaults(), nil
}

func (c Config) withDefaults() Config {
	if c.Suites == "" {
		c.Suites = DefaultSuites
	}
	if c.DB == "" {
		c.DB = DefaultDB
	}
	return c
}

// SuitesPath resolves the suite directory against the config's directory.
func (c Config) SuitesPath() string { return c.resolve(c.Suites) }

// DBPath resolves the database file against the config's directory.
func (c Config) DBPath() string { return c.resolve(c.DB) }

func (c Config) resolve(path string) string {
	if filepath.IsAbs(path) || c.Dir == "" {
		return path
	}
	return filepath.Join(c.Dir, path)
}
