package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

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
	writeConfig(t, root, "name: Board\nprefix: BUG\nstages:\n  - triage\n  - doing\ndefault_agent:\n  command: claude\n  args:\n    - --json\ncron_agents:\n  - name: groomer\n    schedule: \"@every 5m\"\n    command: codex\n    prompt: \"tidy\"\n")

	got, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Name != "Board" || got.Prefix != "BUG" {
		t.Fatalf("unexpected config: %#v", got)
	}
	if len(got.Stages) != 2 || got.Stages[0] != "triage" || got.Stages[1] != "doing" {
		t.Fatalf("unexpected stages: %#v", got.Stages)
	}
	if got.DefaultAgent == nil || got.DefaultAgent.Command != "claude" || len(got.DefaultAgent.Args) != 1 || got.DefaultAgent.Args[0] != "--json" {
		t.Fatalf("unexpected default agent: %#v", got.DefaultAgent)
	}
	if len(got.CronAgents) != 1 || got.CronAgents[0].Name != "groomer" || got.CronAgents[0].Command != "codex" || !got.CronAgents[0].IsEnabled() {
		t.Fatalf("unexpected cron agents: %#v", got.CronAgents)
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
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load() = %#v, want %#v", got, want)
	}
	if _, err := os.Stat(Path(root)); err != nil {
		t.Fatalf("Stat(%q): %v", Path(root), err)
	}
}

func TestSaveLoadCleanupConfig(t *testing.T) {
	root := t.TempDir()
	want := Config{
		Prefix: "TIC",
		Stages: []string{"backlog", "done"},
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

func TestSaveLoadCronAgents(t *testing.T) {
	root := t.TempDir()
	disabled := false
	want := Config{
		Prefix: "TIC",
		Stages: []string{"backlog", "done"},
		CronAgents: []CronAgentConfig{
			{
				Name:     "backlog-groomer",
				Schedule: "@every 5m",
				Command:  "claude",
				Args:     []string{"--print"},
				Prompt:   "groom",
				Enabled:  &disabled,
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
	if cron.Name != "backlog-groomer" || cron.Schedule != "@every 5m" || cron.Command != "claude" || cron.Prompt != "groom" {
		t.Fatalf("cron agent = %#v", cron)
	}
	if cron.IsEnabled() {
		t.Fatalf("cron agent should be disabled: %#v", cron)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{name: "empty prefix", cfg: Config{Stages: []string{"backlog"}}, wantErr: "prefix is empty"},
		{name: "empty stages", cfg: Config{Prefix: "TIC"}, wantErr: "at least one stage is required"},
		{name: "slash", cfg: Config{Prefix: "TIC", Stages: []string{"back/log"}}, wantErr: "path separators"},
		{name: "backslash", cfg: Config{Prefix: "TIC", Stages: []string{"back\\log"}}, wantErr: "path separators"},
		{name: "dot prefix", cfg: Config{Prefix: "TIC", Stages: []string{".hidden"}}, wantErr: "must not start with a dot"},
		{name: "dot dot", cfg: Config{Prefix: "TIC", Stages: []string{".."}}, wantErr: "must not start with a dot"},
		{name: "duplicate", cfg: Config{Prefix: "TIC", Stages: []string{"backlog", "backlog"}}, wantErr: "duplicate stage"},
		{name: "unknown cleanup stage", cfg: Config{
			Prefix:  "TIC",
			Stages:  []string{"backlog", "done"},
			Cleanup: &CleanupConfig{Stages: []CleanupStage{{Name: "archive", AgentData: true}}},
		}, wantErr: `unknown cleanup stage "archive"`},
		{name: "duplicate cleanup stage", cfg: Config{
			Prefix: "TIC",
			Stages: []string{"backlog", "done"},
			Cleanup: &CleanupConfig{Stages: []CleanupStage{
				{Name: "done", AgentData: true},
				{Name: "done", Worktree: true},
			}},
		}, wantErr: `duplicate cleanup stage "done"`},
		{name: "duplicate cron agent", cfg: Config{
			Prefix: "TIC",
			Stages: []string{"backlog"},
			CronAgents: []CronAgentConfig{
				{Name: "groomer", Schedule: "@every 5m", Command: "claude", Prompt: "x"},
				{Name: "groomer", Schedule: "@every 10m", Command: "claude", Prompt: "y"},
			},
		}, wantErr: `duplicate cron agent "groomer"`},
		{name: "invalid cron schedule", cfg: Config{
			Prefix: "TIC",
			Stages: []string{"backlog"},
			CronAgents: []CronAgentConfig{
				{Name: "groomer", Schedule: "nope", Command: "claude", Prompt: "x"},
			},
		}, wantErr: `invalid schedule`},
		{name: "empty cron command", cfg: Config{
			Prefix: "TIC",
			Stages: []string{"backlog"},
			CronAgents: []CronAgentConfig{
				{Name: "groomer", Schedule: "@every 5m", Prompt: "x"},
			},
		}, wantErr: `command is empty`},
		{name: "empty cron prompt", cfg: Config{
			Prefix: "TIC",
			Stages: []string{"backlog"},
			CronAgents: []CronAgentConfig{
				{Name: "groomer", Schedule: "@every 5m", Command: "claude"},
			},
		}, wantErr: `prompt is empty`},
		{name: "cron worktree unsupported", cfg: Config{
			Prefix: "TIC",
			Stages: []string{"backlog"},
			CronAgents: []CronAgentConfig{
				{Name: "groomer", Schedule: "@every 5m", Command: "claude", Prompt: "x", Worktree: true},
			},
		}, wantErr: `worktree=true is not supported yet`},
		{name: "cron base branch without worktree", cfg: Config{
			Prefix: "TIC",
			Stages: []string{"backlog"},
			CronAgents: []CronAgentConfig{
				{Name: "groomer", Schedule: "@every 5m", Command: "claude", Prompt: "x", BaseBranch: "main"},
			},
		}, wantErr: `base_branch requires worktree=true`},
		{name: "ok", cfg: Config{Prefix: "TIC", Stages: []string{"backlog", "done"}}},
		{name: "ok with cron", cfg: Config{
			Prefix: "TIC",
			Stages: []string{"backlog", "done"},
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
	if !cfg.HasStage("prep") {
		t.Fatal("HasStage(prep) = false, want true")
	}
	if cfg.HasStage("nope") {
		t.Fatal("HasStage(nope) = true, want false")
	}
	if cfg.HasDefaultAgent() {
		t.Fatal("HasDefaultAgent() = true, want false")
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
}
