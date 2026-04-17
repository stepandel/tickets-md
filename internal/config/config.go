// Package config loads and writes the .tickets/config.yml file that
// configures the on-disk ticket store: where tickets live, the ID
// prefix, and the ordered list of stages (folders).
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

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

type Duration struct {
	time.Duration
}

func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	var value string
	if err := node.Decode(&value); err != nil {
		return err
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fmt.Errorf("parse duration %q: %w", value, err)
	}
	d.Duration = parsed
	return nil
}

func (d Duration) MarshalYAML() (any, error) {
	return d.String(), nil
}

type WatchConfig struct {
	PollInterval   *Duration `yaml:"poll_interval,omitempty"`
	IdleBlockAfter *Duration `yaml:"idle_block_after,omitempty"`
}

type WorktreesConfig struct {
	Dir          string `yaml:"dir,omitempty"`
	BranchPrefix string `yaml:"branch_prefix,omitempty"`
}

type CronAgentConfig struct {
	Name        string   `yaml:"name"`
	Schedule    string   `yaml:"schedule"`
	Command     string   `yaml:"command"`
	Args        []string `yaml:"args,omitempty"`
	Prompt      string   `yaml:"prompt"`
	Interactive bool     `yaml:"interactive,omitempty"`
	Worktree    bool     `yaml:"worktree,omitempty"`
	BaseBranch  string   `yaml:"base_branch,omitempty"`
	Enabled     *bool    `yaml:"enabled,omitempty"`
}

func (c CronAgentConfig) IsEnabled() bool {
	return c.Enabled == nil || *c.Enabled
}

type PriorityConfig struct {
	Color string `yaml:"color"`
	Bold  bool   `yaml:"bold,omitempty"`
	Order *int   `yaml:"order,omitempty"`
}

// Config describes a ticket store layout. The store always lives
// under `<root>/.tickets/`, so the only things worth configuring are
// the ID prefix and the stage list.
type Config struct {
	// Name is an optional display name for the board.
	Name string `yaml:"name,omitempty"`
	// Prefix is the alphabetic prefix used in ticket IDs, e.g. "TIC".
	Prefix string `yaml:"prefix"`
	// ProjectPrefix is the alphabetic prefix used in project IDs.
	ProjectPrefix string `yaml:"project_prefix,omitempty"`
	// Stages is the ordered list of stage folder names. The first
	// entry is treated as the default stage for new tickets.
	Stages []string `yaml:"stages"`
	// CompleteStages configures which stages count as complete for
	// automatic unblocking of dependent tickets on Move.
	CompleteStages []string `yaml:"complete_stages,omitempty"`
	// ArchiveStage is an optional configured stage hidden from default
	// list and board views.
	ArchiveStage string `yaml:"archive_stage,omitempty"`
	// DefaultAgent is the agent command used by `tickets agents run`.
	DefaultAgent *DefaultAgentConfig `yaml:"default_agent,omitempty"`
	// Cleanup configures the optional `tickets cleanup` stage sweep.
	Cleanup *CleanupConfig `yaml:"cleanup,omitempty"`
	// Watch configures the optional `tickets watch` monitor timing.
	Watch *WatchConfig `yaml:"watch,omitempty"`
	// Worktrees configures where per-ticket git worktrees live and how
	// their branches are prefixed.
	Worktrees *WorktreesConfig `yaml:"worktrees,omitempty"`
	// CronAgents configures board-level agents fired by `tickets watch`
	// on a schedule rather than by ticket filesystem events.
	CronAgents []CronAgentConfig `yaml:"cron_agents,omitempty"`
	// Priorities configures board priority styling by normalized priority
	// name. When absent, built-in defaults are used.
	Priorities map[string]PriorityConfig `yaml:"priorities,omitempty"`
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
		Prefix:        "TIC",
		ProjectPrefix: "PRJ",
		Stages:        []string{"backlog", "prep", "execute", "review", "done"},
	}
}

func DefaultPriorities() map[string]PriorityConfig {
	return map[string]PriorityConfig{
		"critical": {Color: "#FF5F5F", Bold: true},
		"urgent":   {Color: "#FF5F5F", Bold: true},
		"high":     {Color: "#FF8C00", Bold: true},
		"medium":   {Color: "#FFD700"},
		"med":      {Color: "#FFD700"},
		"low":      {Color: "#888888"},
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
	if c.ProjectPrefix == "" {
		c.ProjectPrefix = "PRJ"
	}
	if c.Worktrees == nil {
		c.Worktrees = &WorktreesConfig{}
	}
	if c.Worktrees.Dir == "" {
		c.Worktrees.Dir = defaultWorktreeDir
	}
	if c.Worktrees.BranchPrefix == "" {
		c.Worktrees.BranchPrefix = defaultWorktreeBranchPrefix
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
	if c.ProjectPrefix == "" {
		return errors.New("project_prefix is empty")
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
	if c.Watch != nil {
		if c.Watch.PollInterval != nil && c.Watch.PollInterval.Duration <= 0 {
			return errors.New("watch.poll_interval must be > 0")
		}
		if c.Watch.IdleBlockAfter != nil {
			if c.Watch.IdleBlockAfter.Duration <= 0 {
				return errors.New("watch.idle_block_after must be > 0")
			}
			if c.Watch.IdleBlockAfter.Duration < time.Second {
				return errors.New("watch.idle_block_after must be >= 1s")
			}
		}
	}
	completeSeen := make(map[string]struct{}, len(c.CompleteStages))
	for _, st := range c.CompleteStages {
		if _, dup := completeSeen[st]; dup {
			return fmt.Errorf("duplicate complete stage %q", st)
		}
		if _, ok := seen[st]; !ok {
			return fmt.Errorf("unknown complete stage %q", st)
		}
		completeSeen[st] = struct{}{}
	}
	if c.ArchiveStage != "" && !c.HasStage(c.ArchiveStage) {
		return fmt.Errorf("unknown archive stage %q", c.ArchiveStage)
	}
	if err := validatePriorities(c.Priorities); err != nil {
		return err
	}
	if err := validateWorktreeDir(c.WorktreeDir()); err != nil {
		return err
	}
	if err := validateWorktreeBranchPrefix(c.WorktreeBranchPrefix()); err != nil {
		return err
	}
	return ValidateCronAgents(c.CronAgents)
}

func validatePriorities(priorities map[string]PriorityConfig) error {
	seen := make(map[string]string, len(priorities))
	orderSeen := make(map[int]string, len(priorities))
	for name, priority := range priorities {
		normalized := normalizePriorityName(name)
		if normalized == "" {
			return errors.New("priority name is empty")
		}
		if normalized == "none" {
			return fmt.Errorf("priority %q is reserved", name)
		}
		if prev, dup := seen[normalized]; dup {
			return fmt.Errorf("duplicate priority %q conflicts with %q", name, prev)
		}
		seen[normalized] = name
		if strings.TrimSpace(priority.Color) == "" {
			return fmt.Errorf("priority %q color is empty", name)
		}
		if priority.Order != nil {
			if prev, dup := orderSeen[*priority.Order]; dup {
				return fmt.Errorf("priority %q order %d conflicts with %q", name, *priority.Order, prev)
			}
			orderSeen[*priority.Order] = name
		}
	}
	return nil
}

func ValidateCronAgents(cronAgents []CronAgentConfig) error {
	seen := make(map[string]struct{}, len(cronAgents))
	parser := cronParser()
	for _, ca := range cronAgents {
		if err := ValidateCronName(ca.Name); err != nil {
			return err
		}
		if _, dup := seen[ca.Name]; dup {
			return fmt.Errorf("duplicate cron agent %q", ca.Name)
		}
		seen[ca.Name] = struct{}{}
		if ca.Schedule == "" {
			return fmt.Errorf("cron agent %q schedule is empty", ca.Name)
		}
		if _, err := parser.Parse(ca.Schedule); err != nil {
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
	if name == "projects" {
		return fmt.Errorf("stage name %q is reserved", name)
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

const (
	defaultWorktreeDir          = ".worktrees"
	defaultWorktreeBranchPrefix = "tickets/"
)

// DefaultStage returns the first stage, used when creating new tickets.
func (c Config) DefaultStage() string { return c.Stages[0] }

func (c Config) WorktreeDir() string {
	if c.Worktrees == nil || c.Worktrees.Dir == "" {
		return defaultWorktreeDir
	}
	return c.Worktrees.Dir
}

func (c Config) WorktreeBranchPrefix() string {
	if c.Worktrees == nil || c.Worktrees.BranchPrefix == "" {
		return defaultWorktreeBranchPrefix
	}
	return c.Worktrees.BranchPrefix
}

// HasStage reports whether name is one of the configured stages.
func (c Config) HasStage(name string) bool {
	for _, s := range c.Stages {
		if s == name {
			return true
		}
	}
	return false
}

func (c Config) HasArchiveStage() bool {
	return c.ArchiveStage != ""
}

func (c Config) IsArchiveStage(name string) bool {
	return c.ArchiveStage != "" && c.ArchiveStage == name
}

func (c Config) IsCompleteStage(name string) bool {
	for _, s := range c.CompleteStages {
		if s == name {
			return true
		}
	}
	return false
}

func (c Config) LookupPriority(name string) (PriorityConfig, bool) {
	normalized := normalizePriorityName(name)
	if normalized == "" {
		return PriorityConfig{}, false
	}
	if c.Priorities != nil {
		for key, priority := range c.Priorities {
			if normalizePriorityName(key) == normalized {
				return priority, true
			}
		}
		return PriorityConfig{}, false
	}
	priority, ok := DefaultPriorities()[normalized]
	return priority, ok
}

func (c Config) OrderedPriorityNames() []string {
	if c.Priorities == nil {
		return []string{"critical", "high", "medium", "low"}
	}

	type entry struct {
		name       string
		normalized string
		order      *int
	}

	entries := make([]entry, 0, len(c.Priorities))
	for name, priority := range c.Priorities {
		entries = append(entries, entry{
			name:       name,
			normalized: normalizePriorityName(name),
			order:      priority.Order,
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		left := entries[i]
		right := entries[j]
		switch {
		case left.order != nil && right.order != nil:
			if *left.order != *right.order {
				return *left.order < *right.order
			}
		case left.order != nil:
			return true
		case right.order != nil:
			return false
		}
		return left.normalized < right.normalized
	})

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.name)
	}
	return names
}

func normalizePriorityName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func validateWorktreeDir(dir string) error {
	if strings.TrimSpace(dir) == "" {
		return errors.New("worktrees.dir is empty")
	}
	if filepath.IsAbs(dir) {
		return fmt.Errorf("worktrees.dir %q must be relative", dir)
	}
	clean := filepath.Clean(dir)
	if clean == "." || clean == ".." {
		return fmt.Errorf("worktrees.dir %q is not allowed", dir)
	}
	if strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("worktrees.dir %q must not escape the repo root", dir)
	}
	return nil
}

func validateWorktreeBranchPrefix(prefix string) error {
	if strings.TrimSpace(prefix) == "" {
		return errors.New("worktrees.branch_prefix is empty")
	}
	if strings.ContainsAny(prefix, " \t\r\n") {
		return fmt.Errorf("worktrees.branch_prefix %q must not contain whitespace", prefix)
	}
	if !strings.HasSuffix(prefix, "/") {
		return fmt.Errorf("worktrees.branch_prefix %q must end with /", prefix)
	}
	return nil
}
