package cli

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"

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

func newReloadWatcher(t *testing.T, root string, stages []string) *fsnotify.Watcher {
	t.Helper()
	w, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })
	for _, st := range stages {
		if err := w.Add(filepath.Join(root, config.ConfigDir, st)); err != nil {
			t.Fatalf("Add(%s): %v", st, err)
		}
	}
	return w
}

func watchListContains(w *fsnotify.Watcher, path string) bool {
	return slices.Contains(w.WatchList(), path)
}

// sortedWatchList returns w.WatchList() sorted. fsnotify ranges an
// internal map to build it, so iteration order is not stable — any
// equality check against a prior snapshot must sort first.
func sortedWatchList(w *fsnotify.Watcher) []string {
	list := w.WatchList()
	slices.Sort(list)
	return list
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

	timings, changed, prevStages, nextStages, err := reloadWatchConfig(s.Root, &s.Config, mon, func(cfg config.Config) error { return nil })
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
	if !slices.Equal(prevStages, []string{"backlog", "execute", "done"}) {
		t.Fatalf("prevStages = %#v, want initial stages", prevStages)
	}
	if !slices.Equal(nextStages, []string{"backlog", "execute", "done"}) {
		t.Fatalf("nextStages = %#v, want unchanged stages", nextStages)
	}
}

func TestReloadWatchConfigInvalidConfigPreservesMonitorTiming(t *testing.T) {
	s := newReloadWatchStore(t, config.Config{
		Prefix:        "TIC",
		ProjectPrefix: "PRJ",
		Stages:        []string{"backlog", "execute", "done"},
	})
	mon := agent.NewMonitor(s.Root, 5*time.Second, 30*time.Second, 10*time.Minute, func(string) bool { return false }, func(string) int { return -1 }, func(string) error { return nil })
	w := newReloadWatcher(t, s.Root, s.Config.Stages)

	// Bypass Save's Validate to put an invalid config on disk, so we
	// actually exercise reloadWatchConfig's Load-failure branch.
	invalid := []byte("prefix: TIC\nproject_prefix: PRJ\nstages: [backlog, execute, done]\nwatch:\n  poll_interval: notaduration\n")
	if err := os.WriteFile(config.Path(s.Root), invalid, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	pollBefore, idleBefore, killBefore := mon.Timing()
	stagesBefore := slices.Clone(s.Config.Stages)
	watchesBefore := sortedWatchList(w)
	_, changed, _, _, err := reloadWatchConfig(s.Root, &s.Config, mon, func(cfg config.Config) error {
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
	if !slices.Equal(s.Config.Stages, stagesBefore) {
		t.Fatalf("Stages = %v, want unchanged %v", s.Config.Stages, stagesBefore)
	}
	if got := sortedWatchList(w); !slices.Equal(got, watchesBefore) {
		t.Fatalf("WatchList = %v, want unchanged %v", got, watchesBefore)
	}
}

func TestReloadWatchConfigCronReloadFailurePreservesMonitorTiming(t *testing.T) {
	s := newReloadWatchStore(t, config.Config{
		Prefix:        "TIC",
		ProjectPrefix: "PRJ",
		Stages:        []string{"backlog", "execute", "done"},
	})
	mon := agent.NewMonitor(s.Root, 5*time.Second, 30*time.Second, 10*time.Minute, func(string) bool { return false }, func(string) int { return -1 }, func(string) error { return nil })
	w := newReloadWatcher(t, s.Root, s.Config.Stages)

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
	stagesBefore := slices.Clone(s.Config.Stages)
	watchesBefore := sortedWatchList(w)
	_, changed, _, _, err := reloadWatchConfig(s.Root, &s.Config, mon, func(cfg config.Config) error { return reloadErr })
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
	if !slices.Equal(s.Config.Stages, stagesBefore) {
		t.Fatalf("Stages = %v, want unchanged %v", s.Config.Stages, stagesBefore)
	}
	if got := sortedWatchList(w); !slices.Equal(got, watchesBefore) {
		t.Fatalf("WatchList = %v, want unchanged %v", got, watchesBefore)
	}
}

func TestReconcileWatchStagesAddsStage(t *testing.T) {
	s := newReloadWatchStore(t, config.Config{
		Prefix:        "TIC",
		ProjectPrefix: "PRJ",
		Stages:        []string{"backlog", "done"},
	})
	w := newReloadWatcher(t, s.Root, s.Config.Stages)
	stageConfigs := newStageConfigStore()
	stageConfigs.Set("backlog", stage.Config{})
	stageConfigs.Set("done", stage.Config{})

	executeDir := filepath.Join(s.Root, config.ConfigDir, "execute")
	if err := os.MkdirAll(executeDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(executeDir, "TIC-007.md"), []byte("---\ntitle: Added\n---\n"), 0o644); err != nil {
		t.Fatalf("WriteFile ticket: %v", err)
	}
	if err := os.WriteFile(filepath.Join(executeDir, ".stage.yml"), []byte("agent:\n  command: /bin/sh\n  prompt: go\n"), 0o644); err != nil {
		t.Fatalf("WriteFile stage config: %v", err)
	}

	knownPaths := map[string]bool{}
	added, removed := reconcileStageWatchSet(w, s.Root, stageConfigs, knownPaths, s.Config.Stages, []string{"backlog", "execute", "done"})

	if added != 1 || removed != 0 {
		t.Fatalf("added/removed = %d/%d, want 1/0", added, removed)
	}
	if !watchListContains(w, executeDir) {
		t.Fatalf("WatchList = %v, want %s added", w.WatchList(), executeDir)
	}
	got, ok := stageConfigs.Get("execute")
	if !ok {
		t.Fatal("stage config missing for execute")
	}
	if !got.HasAgent() || got.Agent.Command != "/bin/sh" {
		t.Fatalf("stage config = %#v, want /bin/sh agent", got)
	}
	ticketPath := filepath.Join(executeDir, "TIC-007.md")
	if !knownPaths[ticketPath] {
		t.Fatalf("knownPaths missing %s", ticketPath)
	}
}

func TestReconcileWatchStagesRemovesStage(t *testing.T) {
	s := newReloadWatchStore(t, config.Config{
		Prefix:        "TIC",
		ProjectPrefix: "PRJ",
		Stages:        []string{"backlog", "execute", "done"},
	})
	w := newReloadWatcher(t, s.Root, s.Config.Stages)
	stageConfigs := newStageConfigStore()
	for _, st := range s.Config.Stages {
		stageConfigs.Set(st, stage.Config{})
	}

	executeDir := filepath.Join(s.Root, config.ConfigDir, "execute")
	executeTicket := filepath.Join(executeDir, "TIC-007.md")
	backlogTicket := filepath.Join(s.Root, config.ConfigDir, "backlog", "TIC-001.md")
	knownPaths := map[string]bool{
		executeTicket: true,
		backlogTicket: true,
	}

	added, removed := reconcileStageWatchSet(w, s.Root, stageConfigs, knownPaths, s.Config.Stages, []string{"backlog", "done"})

	if added != 0 || removed != 1 {
		t.Fatalf("added/removed = %d/%d, want 0/1", added, removed)
	}
	if watchListContains(w, executeDir) {
		t.Fatalf("WatchList = %v, did not expect %s", w.WatchList(), executeDir)
	}
	if _, ok := stageConfigs.Get("execute"); ok {
		t.Fatal("execute stage config still cached")
	}
	if knownPaths[executeTicket] {
		t.Fatalf("knownPaths still contains %s", executeTicket)
	}
	if !knownPaths[backlogTicket] {
		t.Fatalf("knownPaths dropped unrelated path %s", backlogTicket)
	}
}

func TestReloadWatchConfigReconcilesStageSet(t *testing.T) {
	s := newReloadWatchStore(t, config.Config{
		Prefix:         "TIC",
		ProjectPrefix:  "PRJ",
		Stages:         []string{"backlog", "done"},
		CompleteStages: []string{"done"},
	})
	w := newReloadWatcher(t, s.Root, s.Config.Stages)
	stageConfigs := newStageConfigStore()
	stageConfigs.Set("backlog", stage.Config{})
	stageConfigs.Set("done", stage.Config{})

	doneDir := filepath.Join(s.Root, config.ConfigDir, "done")
	executeDir := filepath.Join(s.Root, config.ConfigDir, "execute")
	executeTicket := filepath.Join(executeDir, "TIC-009.md")
	if err := os.MkdirAll(executeDir, 0o755); err != nil {
		t.Fatalf("MkdirAll execute: %v", err)
	}
	if err := os.WriteFile(filepath.Join(executeDir, "TIC-009.md"), []byte("---\ntitle: Added\n---\n"), 0o644); err != nil {
		t.Fatalf("WriteFile ticket: %v", err)
	}
	if err := os.WriteFile(filepath.Join(executeDir, ".stage.yml"), []byte("cleanup:\n  worktree: true\n"), 0o644); err != nil {
		t.Fatalf("WriteFile stage config: %v", err)
	}

	knownPaths := map[string]bool{
		filepath.Join(doneDir, "TIC-003.md"): true,
	}
	mon := agent.NewMonitor(s.Root, 0, 0, 0, func(string) bool { return false }, func(string) int { return -1 }, func(string) error { return nil })

	nextCfg := s.Config
	nextCfg.Stages = []string{"backlog", "execute"}
	nextCfg.CompleteStages = []string{"execute"}
	nextCfg.ArchiveStage = "execute"
	if err := config.Save(s.Root, nextCfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	_, changed, prevStages, nextStages, err := reloadWatchConfig(s.Root, &s.Config, mon, func(cfg config.Config) error { return nil })
	if err != nil {
		t.Fatalf("reloadWatchConfig: %v", err)
	}
	added, removed := reconcileStageWatchSet(w, s.Root, stageConfigs, knownPaths, prevStages, nextStages)
	if changed {
		t.Fatal("changed = true, want false")
	}
	if added != 1 || removed != 1 {
		t.Fatalf("added/removed = %d/%d, want 1/1", added, removed)
	}
	if !slices.Equal(s.Config.Stages, []string{"backlog", "execute"}) {
		t.Fatalf("Stages = %v, want [backlog execute]", s.Config.Stages)
	}
	if !slices.Equal(s.Config.CompleteStages, []string{"execute"}) {
		t.Fatalf("CompleteStages = %v, want [execute]", s.Config.CompleteStages)
	}
	if s.Config.ArchiveStage != "execute" {
		t.Fatalf("ArchiveStage = %q, want execute", s.Config.ArchiveStage)
	}
	if watchListContains(w, doneDir) {
		t.Fatalf("WatchList = %v, did not expect %s", w.WatchList(), doneDir)
	}
	if !watchListContains(w, executeDir) {
		t.Fatalf("WatchList = %v, want %s", w.WatchList(), executeDir)
	}
	if _, ok := stageConfigs.Get("done"); ok {
		t.Fatal("done stage config still cached")
	}
	got, ok := stageConfigs.Get("execute")
	if !ok {
		t.Fatal("execute stage config missing")
	}
	if !got.HasCleanup() {
		t.Fatalf("execute stage config = %#v, want cleanup config", got)
	}
	if !knownPaths[executeTicket] {
		t.Fatalf("knownPaths missing %s", executeTicket)
	}
	if knownPaths[filepath.Join(doneDir, "TIC-003.md")] {
		t.Fatalf("knownPaths still contains removed-stage path %s", filepath.Join(doneDir, "TIC-003.md"))
	}
}

func TestReloadWatchConfigPropagatesStages(t *testing.T) {
	s := newReloadWatchStore(t, config.Config{
		Prefix:         "TIC",
		ProjectPrefix:  "PRJ",
		Stages:         []string{"backlog", "execute", "done"},
		CompleteStages: []string{"done"},
	})
	mon := agent.NewMonitor(s.Root, 0, 0, 0, func(string) bool { return false }, func(string) int { return -1 }, func(string) error { return nil })

	nextCfg := s.Config
	nextCfg.Stages = []string{"backlog", "execute", "review", "shipped"}
	nextCfg.CompleteStages = []string{"review", "shipped"}
	if err := config.Save(s.Root, nextCfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	_, _, prevStages, nextStages, err := reloadWatchConfig(s.Root, &s.Config, mon, func(cfg config.Config) error { return nil })
	if err != nil {
		t.Fatalf("reloadWatchConfig: %v", err)
	}
	if !slices.Equal(prevStages, []string{"backlog", "execute", "done"}) {
		t.Fatalf("prevStages = %#v, want old stages", prevStages)
	}
	if !slices.Equal(nextStages, []string{"backlog", "execute", "review", "shipped"}) {
		t.Fatalf("nextStages = %#v, want new stages", nextStages)
	}
	if !slices.Equal(s.Config.Stages, nextCfg.Stages) {
		t.Fatalf("Stages = %#v, want %#v", s.Config.Stages, nextCfg.Stages)
	}
	if !slices.Equal(s.Config.CompleteStages, nextCfg.CompleteStages) {
		t.Fatalf("CompleteStages = %#v, want %#v", s.Config.CompleteStages, nextCfg.CompleteStages)
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

func TestReconcileStageWatchSetAddsStage(t *testing.T) {
	s := newReloadWatchStore(t, config.Config{
		Prefix:        "TIC",
		ProjectPrefix: "PRJ",
		Stages:        []string{"backlog", "execute"},
	})
	w, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	defer w.Close()

	executeDir := filepath.Join(s.Root, ".tickets", "execute")
	if err := w.Add(executeDir); err != nil {
		t.Fatalf("Add(%s): %v", executeDir, err)
	}

	stageConfigs := newStageConfigStore()
	executeCfg := stage.Config{Cleanup: &stage.CleanupConfig{Worktree: true}}
	stageConfigs.Set("execute", executeCfg)
	knownPaths := map[string]bool{
		filepath.Join(executeDir, "TIC-123.md"): true,
	}

	added, removed := reconcileStageWatchSet(w, s.Root, stageConfigs, knownPaths, []string{"backlog", "execute"}, []string{"backlog", "execute", "review"})
	if added != 1 || removed != 0 {
		t.Fatalf("added/removed = %d/%d, want 1/0", added, removed)
	}

	reviewDir := filepath.Join(s.Root, ".tickets", "review")
	if _, err := os.Stat(reviewDir); err != nil {
		t.Fatalf("Stat(%s): %v", reviewDir, err)
	}
	if _, err := os.Stat(filepath.Join(reviewDir, ".stage.yml")); err != nil {
		t.Fatalf("Stat(review .stage.yml): %v", err)
	}
	cfg, ok := stageConfigs.Get("review")
	if !ok {
		t.Fatal("review stage config missing after reconcile")
	}
	if cfg.HasAgent() || cfg.HasCleanup() {
		t.Fatalf("review config = %#v, want zero config", cfg)
	}
	if !slices.Contains(w.WatchList(), reviewDir) {
		t.Fatalf("WatchList() = %#v, want %s", w.WatchList(), reviewDir)
	}
	gotExecute, ok := stageConfigs.Get("execute")
	if !ok || gotExecute != executeCfg {
		t.Fatalf("execute config = %#v, want %#v", gotExecute, executeCfg)
	}
}

func TestReconcileStageWatchSetRemovesStage(t *testing.T) {
	s := newReloadWatchStore(t, config.Config{
		Prefix:        "TIC",
		ProjectPrefix: "PRJ",
		Stages:        []string{"backlog", "execute", "review"},
	})
	w, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	defer w.Close()

	executeDir := filepath.Join(s.Root, ".tickets", "execute")
	reviewDir := filepath.Join(s.Root, ".tickets", "review")
	for _, dir := range []string{executeDir, reviewDir} {
		if err := w.Add(dir); err != nil {
			t.Fatalf("Add(%s): %v", dir, err)
		}
	}

	stageConfigs := newStageConfigStore()
	stageConfigs.Set("execute", stage.Config{Agent: &stage.AgentConfig{Command: "codex", Prompt: "do it"}})
	stageConfigs.Set("review", stage.Config{Cleanup: &stage.CleanupConfig{Branch: true}})
	knownPaths := map[string]bool{
		filepath.Join(executeDir, "TIC-123.md"): true,
		filepath.Join(reviewDir, "TIC-124.md"):  true,
	}

	added, removed := reconcileStageWatchSet(w, s.Root, stageConfigs, knownPaths, []string{"backlog", "execute", "review"}, []string{"backlog", "execute"})
	if added != 0 || removed != 1 {
		t.Fatalf("added/removed = %d/%d, want 0/1", added, removed)
	}
	if _, ok := stageConfigs.Get("review"); ok {
		t.Fatal("review stage config still present after removal")
	}
	if !knownPaths[filepath.Join(executeDir, "TIC-123.md")] {
		t.Fatal("execute known path removed unexpectedly")
	}
	if knownPaths[filepath.Join(reviewDir, "TIC-124.md")] {
		t.Fatal("review known path still present after removal")
	}
	if slices.Contains(w.WatchList(), reviewDir) {
		t.Fatalf("WatchList() = %#v, should not contain %s", w.WatchList(), reviewDir)
	}
}

func TestReconcileStageWatchSetPreservesUnchangedStages(t *testing.T) {
	s := newReloadWatchStore(t, config.Config{
		Prefix:        "TIC",
		ProjectPrefix: "PRJ",
		Stages:        []string{"backlog", "execute"},
	})
	w, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	defer w.Close()

	executeDir := filepath.Join(s.Root, ".tickets", "execute")
	if err := w.Add(executeDir); err != nil {
		t.Fatalf("Add(%s): %v", executeDir, err)
	}

	stageConfigs := newStageConfigStore()
	executeCfg := stage.Config{Agent: &stage.AgentConfig{Command: "claude", Prompt: "keep"}}
	stageConfigs.Set("execute", executeCfg)
	executePath := filepath.Join(executeDir, "TIC-123.md")
	knownPaths := map[string]bool{executePath: true}

	added, removed := reconcileStageWatchSet(w, s.Root, stageConfigs, knownPaths, []string{"backlog", "execute"}, []string{"backlog", "execute"})
	if added != 0 || removed != 0 {
		t.Fatalf("added/removed = %d/%d, want 0/0", added, removed)
	}
	got, ok := stageConfigs.Get("execute")
	if !ok || got != executeCfg {
		t.Fatalf("execute config = %#v, want %#v", got, executeCfg)
	}
	if !knownPaths[executePath] {
		t.Fatal("execute known path missing after no-op reconcile")
	}
	if !slices.Contains(w.WatchList(), executeDir) {
		t.Fatalf("WatchList() = %#v, want %s", w.WatchList(), executeDir)
	}
}
