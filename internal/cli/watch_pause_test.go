package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stepandel/tickets-md/internal/agent"
	"github.com/stepandel/tickets-md/internal/config"
	"github.com/stepandel/tickets-md/internal/stage"
	"github.com/stepandel/tickets-md/internal/worktree"
)

func TestWatchPauseHelpersRoundTrip(t *testing.T) {
	s := newCLITestStore(t)

	paused, err := watchPauseActive(s.Root)
	if err != nil {
		t.Fatalf("watchPauseActive: %v", err)
	}
	if paused {
		t.Fatal("paused = true, want false")
	}

	if err := writeWatchPause(s.Root, "prepping a release"); err != nil {
		t.Fatalf("writeWatchPause: %v", err)
	}

	state, paused, err := readWatchPause(s.Root)
	if err != nil {
		t.Fatalf("readWatchPause: %v", err)
	}
	if !paused {
		t.Fatal("paused = false, want true")
	}
	if state.PausedAt.IsZero() {
		t.Fatal("PausedAt = zero, want timestamp")
	}
	if state.Reason != "prepping a release" {
		t.Fatalf("Reason = %q, want %q", state.Reason, "prepping a release")
	}

	if err := clearWatchPause(s.Root); err != nil {
		t.Fatalf("clearWatchPause: %v", err)
	}
	if err := clearWatchPause(s.Root); err != nil {
		t.Fatalf("clearWatchPause second call: %v", err)
	}

	paused, err = watchPauseActive(s.Root)
	if err != nil {
		t.Fatalf("watchPauseActive after clear: %v", err)
	}
	if paused {
		t.Fatal("paused = true after clear, want false")
	}
}

func TestHandleCreatePausedStillUnblocksAndCleansUp(t *testing.T) {
	s := newCleanupStoreWithConfig(t, config.Config{
		Prefix:         "TIC",
		ProjectPrefix:  "PRJ",
		Stages:         []string{"backlog", "execute", "done"},
		CompleteStages: []string{"done"},
	})

	blocker, err := s.Create("Blocker")
	if err != nil {
		t.Fatalf("Create blocker: %v", err)
	}
	blocked, err := s.Create("Blocked")
	if err != nil {
		t.Fatalf("Create blocked: %v", err)
	}
	if err := s.Link(blocked.ID, blocker.ID, "blocked_by"); err != nil {
		t.Fatalf("Link: %v", err)
	}

	layout := worktreeLayout(s.Config)
	if _, err := worktree.Create(s.Root, layout, blocker.ID, ""); err != nil {
		t.Fatalf("Create worktree: %v", err)
	}
	if err := writeWatchPause(s.Root, "quiet period"); err != nil {
		t.Fatalf("writeWatchPause: %v", err)
	}

	dst := filepath.Join(s.Root, ".tickets", "done", blocker.ID+".md")
	if err := os.Rename(blocker.Path, dst); err != nil {
		t.Fatalf("Rename: %v", err)
	}

	runner := agent.NewPTYRunner()
	t.Cleanup(runner.Shutdown)
	mon := agent.NewMonitor(s.Root, 0, 0, 0, runner.Alive, runner.IdleSeconds, runner.Kill)

	stageConfigs := newStageConfigStore()
	stageConfigs.Set("done", stage.Config{
		Cleanup: &stage.CleanupConfig{Worktree: true},
		Agent: &stage.AgentConfig{
			Command: "/bin/sh",
			Args:    []string{"-c", "exit 0"},
			Prompt:  "ignored",
		},
	})

	handleCreate(s, stageConfigs, dst, mon, runner)

	blocker, err = s.Get(blocker.ID)
	if err != nil {
		t.Fatalf("Get blocker: %v", err)
	}
	blocked, err = s.Get(blocked.ID)
	if err != nil {
		t.Fatalf("Get blocked: %v", err)
	}
	if len(blocker.Blocks) != 0 {
		t.Fatalf("blocker.Blocks = %v, want empty", blocker.Blocks)
	}
	if len(blocked.BlockedBy) != 0 {
		t.Fatalf("blocked.BlockedBy = %v, want empty", blocked.BlockedBy)
	}
	if _, err := os.Stat(layout.WorktreePath(s.Root, blocker.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("worktree stat err = %v, want not exist", err)
	}
	if _, err := agent.Latest(s.Root, blocker.ID); err == nil {
		t.Fatal("agent.Latest succeeded, want no run while paused")
	}
}

func TestSpawnCronAgentSkippedWhilePaused(t *testing.T) {
	root := t.TempDir()
	if err := writeWatchPause(root, "maintenance"); err != nil {
		t.Fatalf("writeWatchPause: %v", err)
	}

	runner := agent.NewPTYRunner()
	t.Cleanup(runner.Shutdown)
	mon := agent.NewMonitor(root, 0, 0, 0, runner.Alive, runner.IdleSeconds, runner.Kill)

	err := spawnCronAgent(root, config.CronAgentConfig{
		Name:     "groomer",
		Schedule: "@every 5m",
		Command:  "/bin/sh",
		Args:     []string{"-c", "exit 0"},
		Prompt:   "ignored",
	}, mon, runner)
	if err != nil {
		t.Fatalf("spawnCronAgent: %v", err)
	}

	if _, err := agent.CronLatest(root, "groomer"); err == nil {
		t.Fatal("CronLatest succeeded, want no run while paused")
	}
}

func TestRerunStageAgentReturnsPausedError(t *testing.T) {
	s := newWatchStore(t)
	tk, err := s.Create("Alpha")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	tk, err = s.Move(tk.ID, "execute")
	if err != nil {
		t.Fatalf("Move: %v", err)
	}
	if err := writeWatchPause(s.Root, "maintenance"); err != nil {
		t.Fatalf("writeWatchPause: %v", err)
	}

	runner := agent.NewPTYRunner()
	t.Cleanup(runner.Shutdown)
	mon := agent.NewMonitor(s.Root, 0, 0, 0, runner.Alive, runner.IdleSeconds, runner.Kill)

	stageConfigs := newStageConfigStore()
	stageConfigs.Set("execute", stage.Config{
		Agent: &stage.AgentConfig{
			Command: "/bin/sh",
			Args:    []string{"-c", "exit 0"},
			Prompt:  "ignored",
		},
	})

	_, err = rerunStageAgent(tk.ID, false, s, stageConfigs, mon, runner, 0, 0)
	if !errors.Is(err, errWatchPaused) {
		t.Fatalf("rerunStageAgent error = %v, want %v", err, errWatchPaused)
	}
}

func TestRunCronAgentManualReturnsPausedError(t *testing.T) {
	root := t.TempDir()
	if err := writeWatchPause(root, "maintenance"); err != nil {
		t.Fatalf("writeWatchPause: %v", err)
	}

	runner := agent.NewPTYRunner()
	t.Cleanup(runner.Shutdown)
	mon := agent.NewMonitor(root, 0, 0, 0, runner.Alive, runner.IdleSeconds, runner.Kill)

	_, err := runCronAgentManual(root, config.CronAgentConfig{
		Name:     "groomer",
		Schedule: "@every 5m",
		Command:  "/bin/sh",
		Args:     []string{"-c", "exit 0"},
		Prompt:   "ignored",
	}, mon, runner, 0, 0)
	if !errors.Is(err, errWatchPaused) {
		t.Fatalf("runCronAgentManual error = %v, want %v", err, errWatchPaused)
	}
}

func TestWatchPauseResumeStatusCommands(t *testing.T) {
	s := newCLITestStore(t)

	run := func(args ...string) string {
		t.Helper()
		var out bytes.Buffer
		cmd := NewRootCmd()
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs(args)
		if err := cmd.Execute(); err != nil {
			t.Fatalf("Execute %v: %v", args, err)
		}
		return out.String()
	}

	if out := run("-C", s.Root, "watch", "pause", "prepping", "a", "release"); !strings.Contains(out, "watch paused") {
		t.Fatalf("pause output = %q, want pause confirmation", out)
	}
	if out := run("-C", s.Root, "watch", "status"); !strings.Contains(out, "watch is paused") || !strings.Contains(out, "prepping a release") {
		t.Fatalf("status output = %q, want paused status with reason", out)
	}
	if out := run("-C", s.Root, "watch", "resume"); !strings.Contains(out, "watch resumed") {
		t.Fatalf("resume output = %q, want resume confirmation", out)
	}
	if out := run("-C", s.Root, "watch", "status"); !strings.Contains(out, "watch is active") {
		t.Fatalf("status output = %q, want active status", out)
	}
}
