package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func intPtr(v int) *int {
	return &v
}

func writeConfig(t *testing.T, root, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, ConfigDir), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(Path(root), []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := Load(t.TempDir())
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected os.ErrNotExist, got %v", err)
	}
}

func TestLoad_ParseError(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root, "prefix: [\n")

	_, err := Load(root)
	if err == nil || !strings.Contains(err.Error(), "parsing") {
		t.Fatalf("expected parsing error, got %v", err)
	}
}

func TestLoad_InvalidConfig(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root, "prefix: \"\"\nstages:\n  - backlog\n")

	_, err := Load(root)
	if err == nil || !strings.Contains(err.Error(), "invalid config") {
		t.Fatalf("expected invalid config error, got %v", err)
	}
}

func TestLoad_Success(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root, "name: Board\nprefix: BUG\nproject_prefix: PRJ\nstages:\n  - triage\n  - doing\nwatch:\n  poll_interval: 7s\n  idle_block_after: 45s\n  idle_kill_after: 10m\nworktrees:\n  dir: .trees\n  branch_prefix: agent/\ndefault_agent:\n  command: claude\n  args:\n    - --json\ncron_agents:\n  - name: groomer\n    schedule: \"@every 5m\"\n    command: codex\n    prompt: \"tidy\"\n    interactive: true\n")

	got, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Name != "Board" || got.Prefix != "BUG" {
		t.Fatalf("unexpected config: %#v", got)
	}
	if got.ProjectPrefix != "PRJ" {
		t.Fatalf("unexpected project prefix: %#v", got.ProjectPrefix)
	}
	if len(got.Stages) != 2 || got.Stages[0] != "triage" || got.Stages[1] != "doing" {
		t.Fatalf("unexpected stages: %#v", got.Stages)
	}
	if len(got.CompleteStages) != 0 {
		t.Fatalf("unexpected complete stages: %#v", got.CompleteStages)
	}
	if got.Watch == nil || got.Watch.PollInterval == nil || got.Watch.PollInterval.Duration != 7*time.Second {
		t.Fatalf("unexpected watch.poll_interval: %#v", got.Watch)
	}
	if got.Watch.IdleBlockAfter == nil || got.Watch.IdleBlockAfter.Duration != 45*time.Second {
		t.Fatalf("unexpected watch.idle_block_after: %#v", got.Watch)
	}
	if got.Watch.IdleKillAfter == nil || got.Watch.IdleKillAfter.Duration != 10*time.Minute {
		t.Fatalf("unexpected watch.idle_kill_after: %#v", got.Watch)
	}
	if got.WorktreeDir() != ".trees" || got.WorktreeBranchPrefix() != "agent/" {
		t.Fatalf("unexpected worktrees config: %#v", got.Worktrees)
	}
	if got.DefaultAgent == nil || got.DefaultAgent.Command != "claude" || len(got.DefaultAgent.Args) != 1 || got.DefaultAgent.Args[0] != "--json" {
		t.Fatalf("unexpected default agent: %#v", got.DefaultAgent)
	}
	if len(got.CronAgents) != 1 || got.CronAgents[0].Name != "groomer" || got.CronAgents[0].Command != "codex" || !got.CronAgents[0].IsEnabled() || !got.CronAgents[0].Interactive {
		t.Fatalf("unexpected cron agents: %#v", got.CronAgents)
	}
}

func TestLoad_BackfillsMissingProjectPrefix(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root, "prefix: TIC\nstages:\n  - backlog\n  - done\n")

	got, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.ProjectPrefix != "PRJ" {
		t.Fatalf("ProjectPrefix = %q, want PRJ (backfilled)", got.ProjectPrefix)
	}
	if got.WorktreeDir() != ".worktrees" {
		t.Fatalf("WorktreeDir() = %q, want .worktrees", got.WorktreeDir())
	}
	if got.WorktreeBranchPrefix() != "tickets/" {
		t.Fatalf("WorktreeBranchPrefix() = %q, want tickets/", got.WorktreeBranchPrefix())
	}
}

func TestSaveThenLoad(t *testing.T) {
	root := t.TempDir()
	want := Default()

	if err := Save(root, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Prefix != want.Prefix || got.ProjectPrefix != want.ProjectPrefix || !reflect.DeepEqual(got.Stages, want.Stages) {
		t.Fatalf("Load() = %#v, want defaults %#v", got, want)
	}
	if got.WorktreeDir() != ".worktrees" || got.WorktreeBranchPrefix() != "tickets/" {
		t.Fatalf("resolved worktree defaults = (%q, %q)", got.WorktreeDir(), got.WorktreeBranchPrefix())
	}
	if _, err := os.Stat(Path(root)); err != nil {
		t.Fatalf("Stat(%q): %v", Path(root), err)
	}
}

func TestSaveLoadWorktreesConfig(t *testing.T) {
	root := t.TempDir()
	want := Config{
		Prefix:        "TIC",
		ProjectPrefix: "PRJ",
		Stages:        []string{"backlog", "done"},
		Worktrees: &WorktreesConfig{
			Dir:          ".trees",
			BranchPrefix: "agent/",
		},
	}

	if err := Save(root, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.WorktreeDir() != ".trees" || got.WorktreeBranchPrefix() != "agent/" {
		t.Fatalf("resolved worktrees = (%q, %q)", got.WorktreeDir(), got.WorktreeBranchPrefix())
	}
}

func TestSaveLoadCleanupConfig(t *testing.T) {
	root := t.TempDir()
	want := Config{
		Prefix:        "TIC",
		ProjectPrefix: "PRJ",
		Stages:        []string{"backlog", "done"},
		Cleanup: &CleanupConfig{
			Stages: []CleanupStage{
				{
					Name:      "done",
					AgentData: true,
					Worktree:  true,
					Branch:    true,
				},
			},
		},
	}

	if err := Save(root, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Cleanup == nil || len(got.Cleanup.Stages) != 1 {
		t.Fatalf("Cleanup = %#v, want one stage", got.Cleanup)
	}
	stage := got.Cleanup.Stages[0]
	if stage.Name != "done" || !stage.AgentData || !stage.Worktree || !stage.Branch {
		t.Fatalf("cleanup stage = %#v", stage)
	}
}

func TestSaveLoadWatchConfig(t *testing.T) {
	root := t.TempDir()
	want := Config{
		Prefix:        "TIC",
		ProjectPrefix: "PRJ",
		Stages:        []string{"backlog", "done"},
		Watch: &WatchConfig{
			PollInterval:   &Duration{Duration: 5 * time.Second},
			IdleBlockAfter: &Duration{Duration: 30 * time.Second},
			IdleKillAfter:  &Duration{Duration: 10 * time.Minute},
		},
	}

	if err := Save(root, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Watch == nil || got.Watch.PollInterval == nil || got.Watch.IdleBlockAfter == nil || got.Watch.IdleKillAfter == nil {
		t.Fatalf("Watch = %#v, want all durations", got.Watch)
	}
	if got.Watch.PollInterval.Duration != want.Watch.PollInterval.Duration {
		t.Fatalf("poll interval = %s, want %s", got.Watch.PollInterval.Duration, want.Watch.PollInterval.Duration)
	}
	if got.Watch.IdleBlockAfter.Duration != want.Watch.IdleBlockAfter.Duration {
		t.Fatalf("idle block after = %s, want %s", got.Watch.IdleBlockAfter.Duration, want.Watch.IdleBlockAfter.Duration)
	}
	if got.Watch.IdleKillAfter.Duration != want.Watch.IdleKillAfter.Duration {
		t.Fatalf("idle kill after = %s, want %s", got.Watch.IdleKillAfter.Duration, want.Watch.IdleKillAfter.Duration)
	}
}

func TestSaveLoadCompleteStages(t *testing.T) {
	root := t.TempDir()
	want := Config{
		Prefix:         "TIC",
		ProjectPrefix:  "PRJ",
		Stages:         []string{"backlog", "review", "done"},
		CompleteStages: []string{"done"},
	}

	if err := Save(root, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(got.CompleteStages, want.CompleteStages) {
		t.Fatalf("CompleteStages = %#v, want %#v", got.CompleteStages, want.CompleteStages)
	}
}

func TestSaveLoadArchiveStage(t *testing.T) {
	root := t.TempDir()
	want := Config{
		Prefix:        "TIC",
		ProjectPrefix: "PRJ",
		Stages:        []string{"backlog", "done", "archive"},
		ArchiveStage:  "archive",
	}

	if err := Save(root, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.ArchiveStage != want.ArchiveStage {
		t.Fatalf("ArchiveStage = %q, want %q", got.ArchiveStage, want.ArchiveStage)
	}
}

func TestSaveLoadCronAgents(t *testing.T) {
	root := t.TempDir()
	disabled := false
	want := Config{
		Prefix:        "TIC",
		ProjectPrefix: "PRJ",
		Stages:        []string{"backlog", "done"},
		CronAgents: []CronAgentConfig{
			{
				Name:        "backlog-groomer",
				Schedule:    "@every 5m",
				Command:     "claude",
				Args:        []string{"--print"},
				Prompt:      "groom",
				Interactive: true,
				Enabled:     &disabled,
			},
		},
	}

	if err := Save(root, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got.CronAgents) != 1 {
		t.Fatalf("CronAgents = %#v, want one entry", got.CronAgents)
	}
	cron := got.CronAgents[0]
	if cron.Name != "backlog-groomer" || cron.Schedule != "@every 5m" || cron.Command != "claude" || cron.Prompt != "groom" || !cron.Interactive {
		t.Fatalf("cron agent = %#v", cron)
	}
	if cron.IsEnabled() {
		t.Fatalf("cron agent should be disabled: %#v", cron)
	}
}

func TestSaveLoadPriorities(t *testing.T) {
	root := t.TempDir()
	want := Config{
		Prefix:        "TIC",
		ProjectPrefix: "PRJ",
		Stages:        []string{"backlog", "done"},
		Priorities: map[string]PriorityConfig{
			"P0":   {Color: "#ff0000", Bold: true, Order: intPtr(0)},
			"size": {Color: "12", Order: intPtr(4)},
		},
	}

	if err := Save(root, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(got.Priorities, want.Priorities) {
		t.Fatalf("Priorities = %#v, want %#v", got.Priorities, want.Priorities)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{name: "empty prefix", cfg: Config{Stages: []string{"backlog"}}, wantErr: "prefix is empty"},
		{name: "empty project prefix", cfg: Config{Prefix: "TIC", Stages: []string{"backlog"}}, wantErr: "project_prefix is empty"},
		{name: "empty stages", cfg: Config{Prefix: "TIC", ProjectPrefix: "PRJ"}, wantErr: "at least one stage is required"},
		{name: "slash", cfg: Config{Prefix: "TIC", ProjectPrefix: "PRJ", Stages: []string{"back/log"}}, wantErr: "path separators"},
		{name: "backslash", cfg: Config{Prefix: "TIC", ProjectPrefix: "PRJ", Stages: []string{"back\\log"}}, wantErr: "path separators"},
		{name: "dot prefix", cfg: Config{Prefix: "TIC", ProjectPrefix: "PRJ", Stages: []string{".hidden"}}, wantErr: "must not start with a dot"},
		{name: "dot dot", cfg: Config{Prefix: "TIC", ProjectPrefix: "PRJ", Stages: []string{".."}}, wantErr: "must not start with a dot"},
		{name: "reserved projects", cfg: Config{Prefix: "TIC", ProjectPrefix: "PRJ", Stages: []string{"projects"}}, wantErr: "reserved"},
		{name: "duplicate", cfg: Config{Prefix: "TIC", ProjectPrefix: "PRJ", Stages: []string{"backlog", "backlog"}}, wantErr: "duplicate stage"},
		{name: "unknown cleanup stage", cfg: Config{
			Prefix:        "TIC",
			ProjectPrefix: "PRJ",
			Stages:        []string{"backlog", "done"},
			Cleanup:       &CleanupConfig{Stages: []CleanupStage{{Name: "archive", AgentData: true}}},
		}, wantErr: `unknown cleanup stage "archive"`},
		{name: "duplicate cleanup stage", cfg: Config{
			Prefix:        "TIC",
			ProjectPrefix: "PRJ",
			Stages:        []string{"backlog", "done"},
			Cleanup: &CleanupConfig{Stages: []CleanupStage{
				{Name: "done", AgentData: true},
				{Name: "done", Worktree: true},
			}},
		}, wantErr: `duplicate cleanup stage "done"`},
		{name: "unknown complete stage", cfg: Config{
			Prefix:         "TIC",
			ProjectPrefix:  "PRJ",
			Stages:         []string{"backlog", "done"},
			CompleteStages: []string{"archive"},
		}, wantErr: `unknown complete stage "archive"`},
		{name: "duplicate complete stage", cfg: Config{
			Prefix:         "TIC",
			ProjectPrefix:  "PRJ",
			Stages:         []string{"backlog", "done"},
			CompleteStages: []string{"done", "done"},
		}, wantErr: `duplicate complete stage "done"`},
		{name: "watch poll interval must be positive", cfg: Config{
			Prefix:        "TIC",
			ProjectPrefix: "PRJ",
			Stages:        []string{"backlog", "done"},
			Watch:         &WatchConfig{PollInterval: &Duration{}},
		}, wantErr: `watch.poll_interval must be > 0`},
		{name: "watch idle block after must be positive", cfg: Config{
			Prefix:        "TIC",
			ProjectPrefix: "PRJ",
			Stages:        []string{"backlog", "done"},
			Watch:         &WatchConfig{IdleBlockAfter: &Duration{}},
		}, wantErr: `watch.idle_block_after must be > 0`},
		{name: "watch idle block after must be at least one second", cfg: Config{
			Prefix:        "TIC",
			ProjectPrefix: "PRJ",
			Stages:        []string{"backlog", "done"},
			Watch:         &WatchConfig{IdleBlockAfter: &Duration{Duration: 500 * time.Millisecond}},
		}, wantErr: `watch.idle_block_after must be >= 1s`},
		{name: "watch idle kill after must be positive", cfg: Config{
			Prefix:        "TIC",
			ProjectPrefix: "PRJ",
			Stages:        []string{"backlog", "done"},
			Watch:         &WatchConfig{IdleKillAfter: &Duration{}},
		}, wantErr: `watch.idle_kill_after must be > 0`},
		{name: "watch idle kill after must be at least one second", cfg: Config{
			Prefix:        "TIC",
			ProjectPrefix: "PRJ",
			Stages:        []string{"backlog", "done"},
			Watch:         &WatchConfig{IdleKillAfter: &Duration{Duration: 500 * time.Millisecond}},
		}, wantErr: `watch.idle_kill_after must be >= 1s`},
		{name: "watch idle kill after must not be shorter than block after", cfg: Config{
			Prefix:        "TIC",
			ProjectPrefix: "PRJ",
			Stages:        []string{"backlog", "done"},
			Watch: &WatchConfig{
				IdleBlockAfter: &Duration{Duration: 30 * time.Second},
				IdleKillAfter:  &Duration{Duration: 29 * time.Second},
			},
		}, wantErr: `watch.idle_kill_after must be >= watch.idle_block_after`},
		{name: "unknown archive stage", cfg: Config{
			Prefix:        "TIC",
			ProjectPrefix: "PRJ",
			Stages:        []string{"backlog", "done"},
			ArchiveStage:  "archive",
		}, wantErr: `unknown archive stage "archive"`},
		{name: "empty priority color", cfg: Config{
			Prefix:        "TIC",
			ProjectPrefix: "PRJ",
			Stages:        []string{"backlog", "done"},
			Priorities: map[string]PriorityConfig{
				"P0": {Color: ""},
			},
		}, wantErr: `priority "P0" color is empty`},
		{name: "reserved none priority", cfg: Config{
			Prefix:        "TIC",
			ProjectPrefix: "PRJ",
			Stages:        []string{"backlog", "done"},
			Priorities: map[string]PriorityConfig{
				"none": {Color: "gray"},
			},
		}, wantErr: `reserved`},
		{name: "reserved none priority normalized", cfg: Config{
			Prefix:        "TIC",
			ProjectPrefix: "PRJ",
			Stages:        []string{"backlog", "done"},
			Priorities: map[string]PriorityConfig{
				" NONE ": {Color: "gray"},
			},
		}, wantErr: `reserved`},
		{name: "duplicate normalized priority", cfg: Config{
			Prefix:        "TIC",
			ProjectPrefix: "PRJ",
			Stages:        []string{"backlog", "done"},
			Priorities: map[string]PriorityConfig{
				"High":   {Color: "#f00"},
				" high ": {Color: "#0f0"},
			},
		}, wantErr: `duplicate priority`},
		{name: "duplicate priority order", cfg: Config{
			Prefix:        "TIC",
			ProjectPrefix: "PRJ",
			Stages:        []string{"backlog", "done"},
			Priorities: map[string]PriorityConfig{
				"P0": {Color: "#f00", Order: intPtr(1)},
				"P1": {Color: "#0f0", Order: intPtr(1)},
			},
		}, wantErr: `order 1 conflicts`},
		{name: "invalid worktree dir absolute", cfg: Config{
			Prefix:        "TIC",
			ProjectPrefix: "PRJ",
			Stages:        []string{"backlog", "done"},
			Worktrees:     &WorktreesConfig{Dir: "/tmp/worktrees"},
		}, wantErr: `must be relative`},
		{name: "invalid worktree dir escape", cfg: Config{
			Prefix:        "TIC",
			ProjectPrefix: "PRJ",
			Stages:        []string{"backlog", "done"},
			Worktrees:     &WorktreesConfig{Dir: "../worktrees"},
		}, wantErr: `must not escape the repo root`},
		{name: "invalid worktree branch prefix", cfg: Config{
			Prefix:        "TIC",
			ProjectPrefix: "PRJ",
			Stages:        []string{"backlog", "done"},
			Worktrees:     &WorktreesConfig{BranchPrefix: "agent"},
		}, wantErr: `must end with /`},
		{name: "invalid worktree branch whitespace", cfg: Config{
			Prefix:        "TIC",
			ProjectPrefix: "PRJ",
			Stages:        []string{"backlog", "done"},
			Worktrees:     &WorktreesConfig{BranchPrefix: "agent /"},
		}, wantErr: `must not contain whitespace`},
		{name: "duplicate cron agent", cfg: Config{
			Prefix:        "TIC",
			ProjectPrefix: "PRJ",
			Stages:        []string{"backlog"},
			CronAgents: []CronAgentConfig{
				{Name: "groomer", Schedule: "@every 5m", Command: "claude", Prompt: "x"},
				{Name: "groomer", Schedule: "@every 10m", Command: "claude", Prompt: "y"},
			},
		}, wantErr: `duplicate cron agent "groomer"`},
		{name: "invalid cron schedule", cfg: Config{
			Prefix:        "TIC",
			ProjectPrefix: "PRJ",
			Stages:        []string{"backlog"},
			CronAgents: []CronAgentConfig{
				{Name: "groomer", Schedule: "nope", Command: "claude", Prompt: "x"},
			},
		}, wantErr: `invalid schedule`},
		{name: "empty cron command", cfg: Config{
			Prefix:        "TIC",
			ProjectPrefix: "PRJ",
			Stages:        []string{"backlog"},
			CronAgents: []CronAgentConfig{
				{Name: "groomer", Schedule: "@every 5m", Prompt: "x"},
			},
		}, wantErr: `command is empty`},
		{name: "empty cron prompt", cfg: Config{
			Prefix:        "TIC",
			ProjectPrefix: "PRJ",
			Stages:        []string{"backlog"},
			CronAgents: []CronAgentConfig{
				{Name: "groomer", Schedule: "@every 5m", Command: "claude"},
			},
		}, wantErr: `prompt is empty`},
		{name: "cron worktree unsupported", cfg: Config{
			Prefix:        "TIC",
			ProjectPrefix: "PRJ",
			Stages:        []string{"backlog"},
			CronAgents: []CronAgentConfig{
				{Name: "groomer", Schedule: "@every 5m", Command: "claude", Prompt: "x", Worktree: true},
			},
		}, wantErr: `worktree=true is not supported yet`},
		{name: "cron base branch without worktree", cfg: Config{
			Prefix:        "TIC",
			ProjectPrefix: "PRJ",
			Stages:        []string{"backlog"},
			CronAgents: []CronAgentConfig{
				{Name: "groomer", Schedule: "@every 5m", Command: "claude", Prompt: "x", BaseBranch: "main"},
			},
		}, wantErr: `base_branch requires worktree=true`},
		{name: "ok", cfg: Config{Prefix: "TIC", ProjectPrefix: "PRJ", Stages: []string{"backlog", "done"}}},
		{name: "ok with cron", cfg: Config{
			Prefix:        "TIC",
			ProjectPrefix: "PRJ",
			Stages:        []string{"backlog", "done"},
			CronAgents: []CronAgentConfig{
				{Name: "groomer", Schedule: "@every 5m", Command: "claude", Prompt: "x"},
			},
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestLookupPriority(t *testing.T) {
	t.Run("defaults when priorities absent", func(t *testing.T) {
		cfg := Config{}

		got, ok := cfg.LookupPriority(" Critical ")
		if !ok {
			t.Fatal("LookupPriority() = missing, want built-in critical")
		}
		if got.Color != "#FF5F5F" || !got.Bold {
			t.Fatalf("LookupPriority() = %#v, want built-in critical styling", got)
		}

		got, ok = cfg.LookupPriority("med")
		if !ok {
			t.Fatal("LookupPriority() = missing, want built-in med")
		}
		if got.Color != "#FFD700" || got.Bold {
			t.Fatalf("LookupPriority() = %#v, want built-in med styling", got)
		}
	})

	t.Run("configured priorities override defaults", func(t *testing.T) {
		cfg := Config{
			Priorities: map[string]PriorityConfig{
				"P0":   {Color: "#123456", Bold: true},
				"high": {Color: "#654321"},
			},
		}

		got, ok := cfg.LookupPriority(" p0 ")
		if !ok {
			t.Fatal("LookupPriority() = missing, want configured P0")
		}
		if got.Color != "#123456" || !got.Bold {
			t.Fatalf("LookupPriority() = %#v, want configured P0 styling", got)
		}

		got, ok = cfg.LookupPriority("high")
		if !ok {
			t.Fatal("LookupPriority() = missing, want configured high")
		}
		if got.Color != "#654321" || got.Bold {
			t.Fatalf("LookupPriority() = %#v, want configured high styling", got)
		}

		if _, ok := cfg.LookupPriority("critical"); ok {
			t.Fatal("LookupPriority() unexpectedly found built-in priority when config override is present")
		}
	})
}

func TestOrderedPriorityNames(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want []string
	}{
		{
			name: "defaults when priorities absent",
			cfg:  Config{},
			want: []string{"critical", "high", "medium", "low"},
		},
		{
			name: "ordered before unordered custom priorities",
			cfg: Config{
				Priorities: map[string]PriorityConfig{
					"Medium":  {Color: "#333", Order: intPtr(10)},
					"P0":      {Color: "#111", Order: intPtr(0)},
					"z-last":  {Color: "#999"},
					"A first": {Color: "#aaa"},
				},
			},
			want: []string{"P0", "Medium", "A first", "z-last"},
		},
		{
			name: "empty priorities map stays empty",
			cfg: Config{
				Priorities: map[string]PriorityConfig{},
			},
			want: []string{},
		},
		{
			name: "order zero is explicit",
			cfg: Config{
				Priorities: map[string]PriorityConfig{
					"P2": {Color: "#222"},
					"P0": {Color: "#000", Order: intPtr(0)},
				},
			},
			want: []string{"P0", "P2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.OrderedPriorityNames(); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("OrderedPriorityNames() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestValidateCronName(t *testing.T) {
	tests := []struct {
		name     string
		cronName string
		wantErr  string
	}{
		{name: "empty", cronName: "", wantErr: "non-empty"},
		{name: "slash", cronName: "back/log", wantErr: "path separators"},
		{name: "backslash", cronName: "back\\log", wantErr: "path separators"},
		{name: "dot prefix", cronName: ".hidden", wantErr: "must not start with a dot"},
		{name: "dot dot", cronName: "..", wantErr: "must not start with a dot"},
		{name: "ok", cronName: "backlog-groomer"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCronName(tt.cronName)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateCronName() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ValidateCronName() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestValidateStageName(t *testing.T) {
	tests := []struct {
		name      string
		stageName string
		wantErr   string
	}{
		{name: "empty", stageName: "", wantErr: "non-empty"},
		{name: "slash", stageName: "back/log", wantErr: "path separators"},
		{name: "backslash", stageName: "back\\log", wantErr: "path separators"},
		{name: "dot prefix", stageName: ".hidden", wantErr: "must not start with a dot"},
		{name: "dot dot", stageName: "..", wantErr: "must not start with a dot"},
		{name: "reserved", stageName: "projects", wantErr: "reserved"},
		{name: "ok", stageName: "backlog"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateStageName(tt.stageName)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateStageName() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ValidateStageName() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestConfig_Helpers(t *testing.T) {
	cfg := Default()
	if got := cfg.DefaultStage(); got != "backlog" {
		t.Fatalf("DefaultStage() = %q, want backlog", got)
	}
	if cfg.ProjectPrefix != "PRJ" {
		t.Fatalf("ProjectPrefix = %q, want PRJ", cfg.ProjectPrefix)
	}
	if !cfg.HasStage("prep") {
		t.Fatal("HasStage(prep) = false, want true")
	}
	if cfg.HasStage("nope") {
		t.Fatal("HasStage(nope) = true, want false")
	}
	if cfg.IsCompleteStage("done") {
		t.Fatal("IsCompleteStage(done) = true, want false by default")
	}
	if cfg.HasArchiveStage() {
		t.Fatal("HasArchiveStage() = true, want false by default")
	}
	if cfg.IsArchiveStage("done") {
		t.Fatal("IsArchiveStage(done) = true, want false by default")
	}
	if cfg.HasDefaultAgent() {
		t.Fatal("HasDefaultAgent() = true, want false")
	}
	if cfg.WorktreeDir() != ".worktrees" {
		t.Fatalf("WorktreeDir() = %q, want .worktrees", cfg.WorktreeDir())
	}
	if cfg.WorktreeBranchPrefix() != "tickets/" {
		t.Fatalf("WorktreeBranchPrefix() = %q, want tickets/", cfg.WorktreeBranchPrefix())
	}

	cfg.DefaultAgent = &DefaultAgentConfig{Command: "claude"}
	if !cfg.HasDefaultAgent() {
		t.Fatal("HasDefaultAgent() = false, want true")
	}

	cfg.DefaultAgent = &DefaultAgentConfig{}
	if cfg.HasDefaultAgent() {
		t.Fatal("HasDefaultAgent() = true, want false when command is empty")
	}
	if cfg.HasCronAgents() {
		t.Fatal("HasCronAgents() = true, want false")
	}

	cfg.CronAgents = []CronAgentConfig{{Name: "groomer", Schedule: "@every 5m", Command: "claude", Prompt: "x"}}
	if !cfg.HasCronAgents() {
		t.Fatal("HasCronAgents() = false, want true")
	}

	cfg.CompleteStages = []string{"done"}
	if !cfg.IsCompleteStage("done") {
		t.Fatal("IsCompleteStage(done) = false, want true")
	}
	if cfg.IsCompleteStage("prep") {
		t.Fatal("IsCompleteStage(prep) = true, want false")
	}

	cfg.Worktrees = &WorktreesConfig{Dir: ".trees", BranchPrefix: "agent/"}
	if cfg.WorktreeDir() != ".trees" || cfg.WorktreeBranchPrefix() != "agent/" {
		t.Fatalf("custom worktree helpers = (%q, %q)", cfg.WorktreeDir(), cfg.WorktreeBranchPrefix())
	}

	cfg.ArchiveStage = "done"
	if !cfg.HasArchiveStage() {
		t.Fatal("HasArchiveStage() = false, want true")
	}
	if !cfg.IsArchiveStage("done") {
		t.Fatal("IsArchiveStage(done) = false, want true")
	}
	if cfg.IsArchiveStage("prep") {
		t.Fatal("IsArchiveStage(prep) = true, want false")
	}
}
