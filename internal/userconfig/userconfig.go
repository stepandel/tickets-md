// Package userconfig manages the per-user preferences file at
// $XDG_CONFIG_HOME/tickets/config.yml (or ~/.config/tickets/config.yml
// when XDG_CONFIG_HOME is unset).
//
// This is deliberately distinct from the per-store config that lives
// at <root>/.tickets/config.yml. Per-store config describes the store
// (prefix, stages); userconfig holds preferences that follow the
// user across stores — the canonical example being which editor to
// launch for `tickets edit`.
package userconfig

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const (
	dirName  = "tickets"
	fileName = "config.yml"
)

// UserConfig is the on-disk shape of the per-user preferences file.
// New fields should be added with `omitempty` so that an unset
// preference doesn't write a noisy default into the file.
type UserConfig struct {
	// Editor is the command (with optional arguments, e.g. "code -w")
	// to launch for `tickets edit`. Empty means "not chosen yet" —
	// the next interactive `tickets edit` will prompt and save.
	Editor string `yaml:"editor,omitempty"`
}

// Path returns the absolute path to the user config file. It does
// not check whether the file exists.
func Path() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, dirName, fileName), nil
}

// Load reads the user config from disk. A missing file is not an
// error: Load returns a zero-value UserConfig and ok=false so the
// caller can decide whether to prompt the user.
func Load() (UserConfig, bool, error) {
	p, err := Path()
	if err != nil {
		return UserConfig{}, false, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return UserConfig{}, false, nil
		}
		return UserConfig{}, false, err
	}
	var c UserConfig
	if err := yaml.Unmarshal(data, &c); err != nil {
		return UserConfig{}, false, fmt.Errorf("parsing %s: %w", p, err)
	}
	return c, true, nil
}

// Save writes the user config to disk, creating parent directories
// as needed. Callers should treat Save errors as non-fatal warnings:
// "we couldn't remember your choice for next time, but we can still
// honor it for this run."
func Save(c UserConfig) error {
	p, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}
