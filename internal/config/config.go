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

	cron "github.com/robfig/cron/v3"
	"gopkg.in/yaml.v3"
)

// ConfigDir is the directory (relative to the project root) that holds
// the config file.
const ConfigDir = ".tickets"

// ConfigFile is the filename of the config inside ConfigDir.
const ConfigFile = "config.yml"

// DefaultAgentConfig describes the agent command used by `agents run`
// for interactive, on-demand sessions.
type DefaultAgentConfig struct {
	Command string   `yaml:"command"`
	Args    []string `yaml:"args,omitempty"`
}

type CleanupConfig struct {
	Stages []CleanupStage `yaml:"stages,omitempty"`
}

type CleanupStage struct {
	Name      string `yaml:"name"`
	AgentData bool   `yaml:"agent_data,omitempty"`
	Worktree  bool   `yaml:"worktree,omitempty"`
	Branch    bool   `yaml:"branch,omitempty"`
}

type CronAgentConfig struct {
	Name       string   `yaml:"name"`
	Schedule   string   `yaml:"schedule"`
	Command    string   `yaml:"command"`
	Args       []string `yaml:"args,omitempty"`
	Prompt     string   `yaml:"prompt"`
	Worktree   bool     `yaml:"worktree,omitempty"`
	BaseBranch string   `yaml:"base_branch,omitempty"`
	Enabled    *bool    `yaml:"enabled,omitempty"`
}

func (c CronAgentConfig) IsEnabled() bool {
	return c.Enabled == nil || *c.Enabled
}

// Config describes a ticket store layout. The store always lives
// under `<root>/.tickets/`, so the only things worth configuring are
// the ID prefix and the stage list.
type Config struct {
	// Name is an optional display name for the board.
	Name string `yaml:"name,omitempty"`
	// Prefix is the alphabetic prefix used in ticket IDs, e.g. "TIC".
	Prefix string `yaml:"prefix"`
	// Stages is the ordered list of stage folder names. The first
	// entry is treated as the default stage for new tickets.
	Stages []string `yaml:"stages"`
	// DefaultAgent is the agent command used by `tickets agents run`.
	DefaultAgent *DefaultAgentConfig `yaml:"default_agent,omitempty"`
	// Cleanup configures the optional `tickets cleanup` stage sweep.
	Cleanup *CleanupConfig `yaml:"cleanup,omitempty"`
	// CronAgents configures board-level agents fired by `tickets watch`
	// on a schedule rather than by ticket filesystem events.
	CronAgents []CronAgentConfig `yaml:"cron_agents,omitempty"`
}

// HasDefaultAgent reports whether a default agent is configured.
func (c Config) HasDefaultAgent() bool {
	return c.DefaultAgent != nil && c.DefaultAgent.Command != ""
}

func (c Config) HasCronAgents() bool {
	return len(c.CronAgents) > 0
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
	if c.Cleanup != nil {
		cleanupSeen := make(map[string]struct{}, len(c.Cleanup.Stages))
		for _, st := range c.Cleanup.Stages {
			if st.Name == "" {
				return errors.New("cleanup stage name is empty")
			}
			if _, dup := cleanupSeen[st.Name]; dup {
				return fmt.Errorf("duplicate cleanup stage %q", st.Name)
			}
			if _, ok := seen[st.Name]; !ok {
				return fmt.Errorf("unknown cleanup stage %q", st.Name)
			}
			cleanupSeen[st.Name] = struct{}{}
		}
	}
	cronSeen := make(map[string]struct{}, len(c.CronAgents))
	for _, ca := range c.CronAgents {
		if err := ValidateCronName(ca.Name); err != nil {
			return err
		}
		if _, dup := cronSeen[ca.Name]; dup {
			return fmt.Errorf("duplicate cron agent %q", ca.Name)
		}
		cronSeen[ca.Name] = struct{}{}
		if ca.Schedule == "" {
			return fmt.Errorf("cron agent %q schedule is empty", ca.Name)
		}
		if _, err := cronParser().Parse(ca.Schedule); err != nil {
			return fmt.Errorf("cron agent %q has invalid schedule %q: %w", ca.Name, ca.Schedule, err)
		}
		if ca.Command == "" {
			return fmt.Errorf("cron agent %q command is empty", ca.Name)
		}
		if ca.Prompt == "" {
			return fmt.Errorf("cron agent %q prompt is empty", ca.Name)
		}
		if ca.Worktree {
			return fmt.Errorf("cron agent %q worktree=true is not supported yet", ca.Name)
		}
		if ca.BaseBranch != "" && !ca.Worktree {
			return fmt.Errorf("cron agent %q base_branch requires worktree=true", ca.Name)
		}
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

func ValidateCronName(name string) error {
	if name == "" {
		return errors.New("cron agent names must be non-empty")
	}
	if strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("cron agent name %q must not contain path separators", name)
	}
	if strings.HasPrefix(name, ".") {
		return fmt.Errorf("cron agent name %q must not start with a dot", name)
	}
	if name == ".." {
		return fmt.Errorf("cron agent name %q is not allowed", name)
	}
	return nil
}

func cronParser() cron.Parser {
	return cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
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
