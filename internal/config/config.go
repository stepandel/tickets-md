// Package config loads and writes the .tickets/config.yml file that
// configures the on-disk ticket store: where tickets live, the ID
// prefix, and the ordered list of stages (folders).
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ConfigDir is the directory (relative to the project root) that holds
// the config file.
const ConfigDir = ".tickets"

// ConfigFile is the filename of the config inside ConfigDir.
const ConfigFile = "config.yml"

// Config describes a ticket store layout. The store always lives
// under `<root>/.tickets/`, so the only things worth configuring are
// the ID prefix and the stage list.
type Config struct {
	// Prefix is the alphabetic prefix used in ticket IDs, e.g. "TIC".
	Prefix string `yaml:"prefix"`
	// Stages is the ordered list of stage folder names. The first
	// entry is treated as the default stage for new tickets.
	Stages []string `yaml:"stages"`
}

// Default returns the out-of-the-box configuration used by `tickets init`.
func Default() Config {
	return Config{
		Prefix: "TIC",
		Stages: []string{"backlog", "prep", "execute", "review", "done"},
	}
}

// Path returns the absolute path to the config file under root.
func Path(root string) string {
	return filepath.Join(root, ConfigDir, ConfigFile)
}

// Load reads the config from root/.tickets/config.yml. It returns
// os.ErrNotExist (wrapped) if the file is missing so callers can
// suggest running `tickets init`.
func Load(root string) (Config, error) {
	p := Path(root)
	data, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, fmt.Errorf("no ticket store at %s: %w", root, err)
		}
		return Config{}, err
	}
	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return Config{}, fmt.Errorf("parsing %s: %w", p, err)
	}
	if err := c.Validate(); err != nil {
		return Config{}, fmt.Errorf("invalid config %s: %w", p, err)
	}
	return c, nil
}

// Save writes the config to root/.tickets/config.yml, creating the
// directory if needed.
func Save(root string, c Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	dir := filepath.Join(root, ConfigDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, ConfigFile), data, 0o644)
}

// Validate checks that the config is internally consistent.
func (c Config) Validate() error {
	if c.Prefix == "" {
		return errors.New("prefix is empty")
	}
	if len(c.Stages) == 0 {
		return errors.New("at least one stage is required")
	}
	seen := make(map[string]struct{}, len(c.Stages))
	for _, s := range c.Stages {
		if err := ValidateStageName(s); err != nil {
			return err
		}
		if _, dup := seen[s]; dup {
			return fmt.Errorf("duplicate stage %q", s)
		}
		seen[s] = struct{}{}
	}
	return nil
}

// ValidateStageName enforces the rules a stage folder name must
// follow to be safe to create on disk: non-empty, no path separators
// (so it can't escape .tickets/), and no leading dot (so it doesn't
// collide with hidden tooling files like config.yml).
func ValidateStageName(name string) error {
	if name == "" {
		return errors.New("stage names must be non-empty")
	}
	if strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("stage name %q must not contain path separators", name)
	}
	if strings.HasPrefix(name, ".") {
		return fmt.Errorf("stage name %q must not start with a dot", name)
	}
	if name == ".." {
		return fmt.Errorf("stage name %q is not allowed", name)
	}
	return nil
}

// DefaultStage returns the first stage, used when creating new tickets.
func (c Config) DefaultStage() string { return c.Stages[0] }

// HasStage reports whether name is one of the configured stages.
func (c Config) HasStage(name string) bool {
	for _, s := range c.Stages {
		if s == name {
			return true
		}
	}
	return false
}
