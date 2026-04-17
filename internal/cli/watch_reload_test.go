package cli

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stepandel/tickets-md/internal/agent"
	"github.com/stepandel/tickets-md/internal/config"
	"github.com/stepandel/tickets-md/internal/stage"
	"github.com/stepandel/tickets-md/internal/ticket"
)

func newReloadWatchStore(t *testing.T, cfg config.Config) *ticket.Store {
	t.Helper()
	s, err := ticket.Init(t.TempDir(), cfg)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	return s
}

func TestReloadWatchConfigUpdatesMonitorTiming(t *testing.T) {
	s := newReloadWatchStore(t, config.Config{
		Prefix:        "TIC",
		ProjectPrefix: "PRJ",
		Stages:        []string{"backlog", "execute", "done"},
	})
	mon := agent.NewMonitor(s.Root, 0, 0, 0, func(string) bool { return false }, func(string) int { return -1 }, func(string) error { return nil })

	nextCfg := s.Config
	nextCfg.Watch = &config.WatchConfig{
		PollInterval:   &config.Duration{Duration: 7 * time.Second},
		IdleBlockAfter: &config.Duration{Duration: 45 * time.Second},
		IdleKillAfter:  &config.Duration{Duration: 10 * time.Minute},
	}
	nextCfg.CronAgents = []config.CronAgentConfig{
		{Name: "groomer", Schedule: "@every 5m", Command: "/bin/sh", Prompt: "echo hi"},
	}
	if err := config.Save(s.Root, nextCfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	timings, changed, err := reloadWatchConfig(s.Root, &s.Config, mon, func(cfg config.Config) error { return nil })
	if err != nil {
		t.Fatalf("reloadWatchConfig: %v", err)
	}
	if !changed {
		t.Fatal("changed = false, want true")
	}
	if timings.PollInterval != 7*time.Second || timings.IdleBlockAfter != 45*time.Second || timings.IdleKillAfter != 10*time.Minute {
		t.Fatalf("timings = %#v, want 7s/45s/10m", timings)
	}
	pollInterval, idleBlockAfter, idleKillAfter := mon.Timing()
	if pollInterval != 7*time.Second || idleBlockAfter != 45*time.Second || idleKillAfter != 10*time.Minute {
		t.Fatalf("monitor timing = %s/%s/%s, want 7s/45s/10m", pollInterval, idleBlockAfter, idleKillAfter)
	}
	if len(s.Config.CronAgents) != 1 || s.Config.CronAgents[0].Name != "groomer" {
		t.Fatalf("CronAgents = %#v, want groomer", s.Config.CronAgents)
	}
	if s.Config.Watch == nil || s.Config.Watch.PollInterval == nil || s.Config.Watch.PollInterval.Duration != 7*time.Second {
		t.Fatalf("Watch = %#v, want poll interval 7s", s.Config.Watch)
	}
}

func TestReloadWatchConfigInvalidConfigPreservesMonitorTiming(t *testing.T) {
	s := newReloadWatchStore(t, config.Config{
		Prefix:        "TIC",
		ProjectPrefix: "PRJ",
		Stages:        []string{"backlog", "execute", "done"},
	})
	mon := agent.NewMonitor(s.Root, 5*time.Second, 30*time.Second, 10*time.Minute, func(string) bool { return false }, func(string) int { return -1 }, func(string) error { return nil })

	// Bypass Save's Validate to put an invalid config on disk, so we
	// actually exercise reloadWatchConfig's Load-failure branch.
	invalid := []byte("prefix: TIC\nproject_prefix: PRJ\nstages: [backlog, execute, done]\nwatch:\n  poll_interval: notaduration\n")
	if err := os.WriteFile(config.Path(s.Root), invalid, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	pollBefore, idleBefore, killBefore := mon.Timing()
	_, changed, err := reloadWatchConfig(s.Root, &s.Config, mon, func(cfg config.Config) error {
		t.Fatal("reloadCron should not run when config.Load fails")
		return nil
	})
	if err == nil {
		t.Fatal("reloadWatchConfig err = nil, want Load error")
	}
	if changed {
		t.Fatal("changed = true, want false")
	}
	pollAfter, idleAfter, killAfter := mon.Timing()
	if pollAfter != pollBefore || idleAfter != idleBefore || killAfter != killBefore {
		t.Fatalf("monitor timing = %s/%s/%s, want unchanged %s/%s/%s", pollAfter, idleAfter, killAfter, pollBefore, idleBefore, killBefore)
	}
}

func TestReloadWatchConfigCronReloadFailurePreservesMonitorTiming(t *testing.T) {
	s := newReloadWatchStore(t, config.Config{
		Prefix:        "TIC",
		ProjectPrefix: "PRJ",
		Stages:        []string{"backlog", "execute", "done"},
	})
	mon := agent.NewMonitor(s.Root, 5*time.Second, 30*time.Second, 10*time.Minute, func(string) bool { return false }, func(string) int { return -1 }, func(string) error { return nil })

	nextCfg := s.Config
	nextCfg.Watch = &config.WatchConfig{
		PollInterval:   &config.Duration{Duration: 9 * time.Second},
		IdleBlockAfter: &config.Duration{Duration: 50 * time.Second},
		IdleKillAfter:  &config.Duration{Duration: 11 * time.Minute},
	}
	if err := config.Save(s.Root, nextCfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reloadErr := errors.New("cron reload failed")
	pollBefore, idleBefore, killBefore := mon.Timing()
	_, changed, err := reloadWatchConfig(s.Root, &s.Config, mon, func(cfg config.Config) error { return reloadErr })
	if !errors.Is(err, reloadErr) {
		t.Fatalf("reloadWatchConfig error = %v, want %v", err, reloadErr)
	}
	if changed {
		t.Fatal("changed = true, want false")
	}
	pollAfter, idleAfter, killAfter := mon.Timing()
	if pollAfter != pollBefore || idleAfter != idleBefore || killAfter != killBefore {
		t.Fatalf("monitor timing = %s/%s/%s, want unchanged %s/%s/%s", pollAfter, idleAfter, killAfter, pollBefore, idleBefore, killBefore)
	}
	if s.Config.Watch != nil {
		t.Fatalf("Watch = %#v, want unchanged nil", s.Config.Watch)
	}
}

func TestReloadStageConfigUpdatesCachedStage(t *testing.T) {
	s := newReloadWatchStore(t, config.Config{
		Prefix:        "TIC",
		ProjectPrefix: "PRJ",
		Stages:        []string{"backlog", "execute", "done"},
	})
	stageDir := filepath.Join(s.Root, ".tickets", "execute")
	stageConfigs := newStageConfigStore()
	stageConfigs.Set("execute", stage.Config{})

	data := []byte("agent:\n  command: /bin/sh\n  args: [\"-c\", \"exit 0\"]\n  prompt: go\n")
	if err := os.WriteFile(filepath.Join(stageDir, ".stage.yml"), data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := reloadStageConfig(stageConfigs, "execute", stageDir)
	if err != nil {
		t.Fatalf("reloadStageConfig: %v", err)
	}
	if !cfg.HasAgent() || cfg.Agent.Command != "/bin/sh" {
		t.Fatalf("cfg = %#v, want /bin/sh agent", cfg)
	}

	got, ok := stageConfigs.Get("execute")
	if !ok {
		t.Fatal("stage config missing after reload")
	}
	if !got.HasAgent() || got.Agent.Command != "/bin/sh" {
		t.Fatalf("cached config = %#v, want /bin/sh agent", got)
	}
}

func TestReloadStageConfigInvalidYAMLPreservesCachedStage(t *testing.T) {
	stageConfigs := newStageConfigStore()
	want := stage.Config{
		Agent: &stage.AgentConfig{
			Command: "claude",
			Prompt:  "keep me",
		},
	}
	stageConfigs.Set("execute", want)

	stageDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(stageDir, ".stage.yml"), []byte("agent: ["), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := reloadStageConfig(stageConfigs, "execute", stageDir); err == nil {
		t.Fatal("reloadStageConfig err = nil, want parse error")
	}

	got, ok := stageConfigs.Get("execute")
	if !ok {
		t.Fatal("stage config missing after failed reload")
	}
	if got != want {
		t.Fatalf("cached config = %#v, want %#v", got, want)
	}
}

func TestReloadStageConfigDeletionResetsToZeroConfig(t *testing.T) {
	stageConfigs := newStageConfigStore()
	stageDir := t.TempDir()
	stageConfigs.Set("execute", stage.Config{
		Cleanup: &stage.CleanupConfig{Worktree: true},
	})

	data := []byte("cleanup:\n  worktree: true\n  branch: true\n")
	configPath := filepath.Join(stageDir, ".stage.yml")
	if err := os.WriteFile(configPath, data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := reloadStageConfig(stageConfigs, "execute", stageDir); err != nil {
		t.Fatalf("initial reloadStageConfig: %v", err)
	}
	if err := os.Remove(configPath); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	cfg, err := reloadStageConfig(stageConfigs, "execute", stageDir)
	if err != nil {
		t.Fatalf("reloadStageConfig after delete: %v", err)
	}
	if cfg.HasAgent() || cfg.HasCleanup() {
		t.Fatalf("cfg = %#v, want zero config", cfg)
	}

	got, ok := stageConfigs.Get("execute")
	if !ok {
		t.Fatal("stage config missing after delete reload")
	}
	if got.HasAgent() || got.HasCleanup() {
		t.Fatalf("cached config = %#v, want zero config", got)
	}
}
