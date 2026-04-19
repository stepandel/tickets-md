package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stepandel/tickets-md/internal/agent"
	"github.com/stepandel/tickets-md/internal/config"
	"github.com/stepandel/tickets-md/internal/stage"
	"github.com/stepandel/tickets-md/internal/ticket"
)

func newWatchStore(t *testing.T) *ticket.Store {
	t.Helper()
	root := t.TempDir()
	s, err := ticket.Init(root, config.Config{
		Prefix:         "TIC",
		ProjectPrefix:  "PRJ",
		Stages:         []string{"backlog", "execute", "done"},
		CompleteStages: []string{"done"},
	})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	return s
}

func waitForRunStatus(t *testing.T, root, ticketID string, want agent.Status) agent.AgentStatus {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		as, err := agent.Latest(root, ticketID)
		if err == nil && as.Status == want {
			syncAgentFrontmatter(root, ticketID)
			return as
		}
		time.Sleep(20 * time.Millisecond)
	}
	as, err := agent.Latest(root, ticketID)
	if err != nil {
		t.Fatalf("Latest(%s): %v", ticketID, err)
	}
	t.Fatalf("latest status = %q, want %q", as.Status, want)
	return agent.AgentStatus{}
}

func TestSpawnAgentStartFailureMarksRunErrored(t *testing.T) {
	s := newWatchStore(t)
	tk, err := s.Create("Alpha")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	tk, err = s.Move(tk.ID, "execute")
	if err != nil {
		t.Fatalf("Move: %v", err)
	}

	runner := agent.NewPTYRunner()
	mon := agent.NewMonitor(s.Root, 0, 0, 0, runner.Alive, runner.IdleSeconds, runner.Kill)
	mon.OnStatusChange = func(ticketID string) {
		syncAgentFrontmatter(s.Root, ticketID)
	}
	layout := worktreeLayout(s.Config)

	_, err = spawnAgent(tk, stage.Config{
		Agent: &stage.AgentConfig{
			Command: "/definitely/missing/tickets-agent",
			Prompt:  "ignored",
		},
	}, s.Root, layout, newStageConfigStore(), mon, runner, 0, 0)
	if err == nil {
		t.Fatal("spawnAgent succeeded, want error")
	}

	as := waitForRunStatus(t, s.Root, tk.ID, agent.StatusErrored)
	if as.RunID != "001-execute" {
		t.Fatalf("run id = %q, want 001-execute", as.RunID)
	}
	if as.Session != tk.ID+"-1" {
		t.Fatalf("session = %q, want %s-1", as.Session, tk.ID)
	}
	got, err := s.Get(tk.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.AgentStatus != string(agent.StatusErrored) {
		t.Fatalf("agent_status = %q, want errored", got.AgentStatus)
	}
	if got.AgentRun != "001-execute" {
		t.Fatalf("agent_run = %q, want 001-execute", got.AgentRun)
	}
	if got.AgentSession != "" {
		t.Fatalf("agent_session = %q, want empty", got.AgentSession)
	}
}

func TestSpawnAgentImmediateExitMarksRunFailed(t *testing.T) {
	s := newWatchStore(t)
	tk, err := s.Create("Alpha")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	tk, err = s.Move(tk.ID, "execute")
	if err != nil {
		t.Fatalf("Move: %v", err)
	}

	runner := agent.NewPTYRunner()
	mon := agent.NewMonitor(s.Root, 0, 0, 0, runner.Alive, runner.IdleSeconds, runner.Kill)
	mon.OnStatusChange = func(ticketID string) {
		syncAgentFrontmatter(s.Root, ticketID)
	}
	layout := worktreeLayout(s.Config)

	session, err := spawnAgent(tk, stage.Config{
		Agent: &stage.AgentConfig{
			Command: "/bin/sh",
			Args:    []string{"-c", "exit 127"},
			Prompt:  "ignored",
		},
	}, s.Root, layout, newStageConfigStore(), mon, runner, 0, 0)
	if err != nil {
		t.Fatalf("spawnAgent: %v", err)
	}
	if session != tk.ID+"-1" {
		t.Fatalf("session = %q, want %s-1", session, tk.ID)
	}

	as := waitForRunStatus(t, s.Root, tk.ID, agent.StatusFailed)
	if as.ExitCode == nil || *as.ExitCode != 127 {
		t.Fatalf("exit code = %v, want 127", as.ExitCode)
	}
	got, err := s.Get(tk.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.AgentStatus != string(agent.StatusFailed) {
		t.Fatalf("agent_status = %q, want failed", got.AgentStatus)
	}
	if got.AgentRun != "001-execute" {
		t.Fatalf("agent_run = %q, want 001-execute", got.AgentRun)
	}
	if got.AgentSession != "" {
		t.Fatalf("agent_session = %q, want empty", got.AgentSession)
	}
}

func TestSpawnAgentImmediateExitMarksRunDone(t *testing.T) {
	s := newWatchStore(t)
	tk, err := s.Create("Alpha")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	tk, err = s.Move(tk.ID, "execute")
	if err != nil {
		t.Fatalf("Move: %v", err)
	}

	runner := agent.NewPTYRunner()
	mon := agent.NewMonitor(s.Root, 0, 0, 0, runner.Alive, runner.IdleSeconds, runner.Kill)
	mon.OnStatusChange = func(ticketID string) {
		syncAgentFrontmatter(s.Root, ticketID)
	}
	layout := worktreeLayout(s.Config)

	session, err := spawnAgent(tk, stage.Config{
		Agent: &stage.AgentConfig{
			Command: "/bin/sh",
			Args:    []string{"-c", "exit 0"},
			Prompt:  "ignored",
		},
	}, s.Root, layout, newStageConfigStore(), mon, runner, 0, 0)
	if err != nil {
		t.Fatalf("spawnAgent: %v", err)
	}
	if session != tk.ID+"-1" {
		t.Fatalf("session = %q, want %s-1", session, tk.ID)
	}

	as := waitForRunStatus(t, s.Root, tk.ID, agent.StatusDone)
	if as.ExitCode == nil || *as.ExitCode != 0 {
		t.Fatalf("exit code = %v, want 0", as.ExitCode)
	}
	got, err := s.Get(tk.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.AgentStatus != string(agent.StatusDone) {
		t.Fatalf("agent_status = %q, want done", got.AgentStatus)
	}
	if got.AgentRun != "001-execute" {
		t.Fatalf("agent_run = %q, want 001-execute", got.AgentRun)
	}
	if got.AgentSession != "" {
		t.Fatalf("agent_session = %q, want empty", got.AgentSession)
	}
}

func TestSpawnAgentIdleKillMarksRunFailed(t *testing.T) {
	s := newWatchStore(t)
	tk, err := s.Create("Alpha")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	tk, err = s.Move(tk.ID, "execute")
	if err != nil {
		t.Fatalf("Move: %v", err)
	}

	runner := agent.NewPTYRunner()
	t.Cleanup(runner.Shutdown)
	mon := agent.NewMonitor(s.Root, 20*time.Millisecond, time.Hour, 2*time.Second, runner.Alive, runner.IdleSeconds, runner.Kill)
	mon.OnStatusChange = func(ticketID string) {
		syncAgentFrontmatter(s.Root, ticketID)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go mon.Run(ctx)
	layout := worktreeLayout(s.Config)

	session, err := spawnAgent(tk, stage.Config{
		Agent: &stage.AgentConfig{
			Command: "/bin/sh",
			Args:    []string{"-c", "printf 'ready\\n'; sleep 30"},
			Prompt:  "ignored",
		},
	}, s.Root, layout, newStageConfigStore(), mon, runner, 0, 0)
	if err != nil {
		t.Fatalf("spawnAgent: %v", err)
	}
	if !runner.Alive(session) {
		t.Fatalf("session %q not alive", session)
	}

	as := waitForRunStatus(t, s.Root, tk.ID, agent.StatusFailed)
	if !strings.Contains(as.Error, "session killed after") {
		t.Fatalf("error = %q, want idle kill detail", as.Error)
	}
	if runner.Alive(session) {
		t.Fatalf("session %q still alive after idle kill", session)
	}

	got, err := s.Get(tk.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.AgentStatus != string(agent.StatusFailed) {
		t.Fatalf("agent_status = %q, want failed", got.AgentStatus)
	}
	if got.AgentRun != "001-execute" {
		t.Fatalf("agent_run = %q, want 001-execute", got.AgentRun)
	}
	if got.AgentSession != "" {
		t.Fatalf("agent_session = %q, want empty", got.AgentSession)
	}
}

func TestHandleCreateFsRenameIntoCompleteStageUnblocksDependents(t *testing.T) {
	s := newWatchStore(t)
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

	dst := filepath.Join(s.Root, ".tickets", "done", blocker.ID+".md")
	if err := os.Rename(blocker.Path, dst); err != nil {
		t.Fatalf("Rename: %v", err)
	}

	stageConfigs := newStageConfigStore()
	stageConfigs.Set("backlog", stage.Config{})
	stageConfigs.Set("execute", stage.Config{})
	stageConfigs.Set("done", stage.Config{})

	handleCreate(s, stageConfigs, dst, nil, nil)

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
}

func TestRerunStageAgentRefusesActiveSessionWithoutForce(t *testing.T) {
	s := newWatchStore(t)
	tk, err := s.Create("Alpha")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	tk, err = s.Move(tk.ID, "execute")
	if err != nil {
		t.Fatalf("Move: %v", err)
	}

	runner := agent.NewPTYRunner()
	t.Cleanup(runner.Shutdown)
	mon := agent.NewMonitor(s.Root, 0, 0, 0, runner.Alive, runner.IdleSeconds, runner.Kill)
	mon.OnStatusChange = func(ticketID string) {
		syncAgentFrontmatter(s.Root, ticketID)
	}
	layout := worktreeLayout(s.Config)

	stageConfigs := newStageConfigStore()
	stageConfigs.Set("execute", stage.Config{
		Agent: &stage.AgentConfig{
			Command: "/bin/sh",
			Args:    []string{"-c", "sleep 30"},
			Prompt:  "ignored",
		},
	})

	sc, _ := stageConfigs.Get("execute")
	session, err := spawnAgent(tk, sc, s.Root, layout, stageConfigs, mon, runner, 0, 0)
	if err != nil {
		t.Fatalf("spawnAgent: %v", err)
	}
	if !runner.Alive(session) {
		t.Fatalf("session %q not alive", session)
	}

	_, err = rerunStageAgent(tk.ID, false, s, stageConfigs, mon, runner, 0, 0)
	if err == nil {
		t.Fatal("rerunStageAgent succeeded, want error")
	}
	if got, want := err.Error(), "agent already running (session "+session+")"; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}

	as, err := agent.Latest(s.Root, tk.ID)
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if as.RunID != "001-execute" {
		t.Fatalf("run id = %q, want 001-execute", as.RunID)
	}
	if as.Session != session {
		t.Fatalf("session = %q, want %q", as.Session, session)
	}

	if err := runner.Kill(session); err != nil {
		t.Fatalf("Kill(%q): %v", session, err)
	}
	waitForRunStatus(t, s.Root, tk.ID, agent.StatusFailed)
}

func TestRerunStageAgentForceReplacesActiveSession(t *testing.T) {
	s := newWatchStore(t)
	tk, err := s.Create("Alpha")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	tk, err = s.Move(tk.ID, "execute")
	if err != nil {
		t.Fatalf("Move: %v", err)
	}

	runner := agent.NewPTYRunner()
	t.Cleanup(runner.Shutdown)
	mon := agent.NewMonitor(s.Root, 0, 0, 0, runner.Alive, runner.IdleSeconds, runner.Kill)
	mon.OnStatusChange = func(ticketID string) {
		syncAgentFrontmatter(s.Root, ticketID)
	}
	layout := worktreeLayout(s.Config)

	stageConfigs := newStageConfigStore()
	stageConfigs.Set("execute", stage.Config{
		Agent: &stage.AgentConfig{
			Command: "/bin/sh",
			Args:    []string{"-c", "sleep 30"},
			Prompt:  "ignored",
		},
	})

	sc, _ := stageConfigs.Get("execute")
	firstSession, err := spawnAgent(tk, sc, s.Root, layout, stageConfigs, mon, runner, 0, 0)
	if err != nil {
		t.Fatalf("spawnAgent: %v", err)
	}
	if !runner.Alive(firstSession) {
		t.Fatalf("session %q not alive", firstSession)
	}

	secondSession, err := rerunStageAgent(tk.ID, true, s, stageConfigs, mon, runner, 0, 0)
	if err != nil {
		t.Fatalf("rerunStageAgent(force): %v", err)
	}
	if secondSession != tk.ID+"-2" {
		t.Fatalf("session = %q, want %s-2", secondSession, tk.ID)
	}
	if runner.Alive(firstSession) {
		t.Fatalf("old session %q still alive", firstSession)
	}
	if !runner.Alive(secondSession) {
		t.Fatalf("new session %q not alive", secondSession)
	}

	deadline := time.Now().Add(5 * time.Second)
	var firstRun agent.AgentStatus
	for time.Now().Before(deadline) {
		firstRun, err = agent.ReadRun(s.Root, tk.ID, "001-execute")
		if err == nil && firstRun.Status == agent.StatusFailed {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("ReadRun(001-execute): %v", err)
	}
	if firstRun.Status != agent.StatusFailed {
		t.Fatalf("first run status = %q, want failed", firstRun.Status)
	}
	if firstRun.Error == "" {
		t.Fatal("first run error is empty, want failure detail")
	}

	secondRun, err := agent.ReadRun(s.Root, tk.ID, "002-execute")
	if err != nil {
		t.Fatalf("ReadRun(002-execute): %v", err)
	}
	if secondRun.Seq != 2 {
		t.Fatalf("seq = %d, want 2", secondRun.Seq)
	}
	if secondRun.Attempt != 2 {
		t.Fatalf("attempt = %d, want 2", secondRun.Attempt)
	}
	if secondRun.Session != secondSession {
		t.Fatalf("session = %q, want %q", secondRun.Session, secondSession)
	}

	got, err := s.Get(tk.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.AgentRun != "002-execute" {
		t.Fatalf("agent_run = %q, want 002-execute", got.AgentRun)
	}
	if got.AgentSession != secondSession {
		t.Fatalf("agent_session = %q, want %q", got.AgentSession, secondSession)
	}

	if err := runner.Kill(secondSession); err != nil {
		t.Fatalf("Kill(%q): %v", secondSession, err)
	}
	waitForRunStatus(t, s.Root, tk.ID, agent.StatusFailed)
}

func TestHandleCreateQueuesWhenStageAtCapacity(t *testing.T) {
	s := newWatchStore(t)
	activeTicket, err := s.Create("Active")
	if err != nil {
		t.Fatalf("Create active: %v", err)
	}
	activeTicket, err = s.Move(activeTicket.ID, "execute")
	if err != nil {
		t.Fatalf("Move active: %v", err)
	}
	queuedTicket, err := s.Create("Queued")
	if err != nil {
		t.Fatalf("Create queued: %v", err)
	}
	queuedPath := filepath.Join(s.Root, ".tickets", "execute", queuedTicket.ID+".md")
	if err := os.Rename(queuedTicket.Path, queuedPath); err != nil {
		t.Fatalf("Rename queued ticket: %v", err)
	}

	runner := agent.NewPTYRunner()
	t.Cleanup(runner.Shutdown)
	mon := agent.NewMonitor(s.Root, 0, 0, 0, runner.Alive, runner.IdleSeconds, runner.Kill)
	stageConfigs := newStageConfigStore()
	stageConfigs.Set("execute", stage.Config{
		Agent: &stage.AgentConfig{
			Command:       "/bin/sh",
			Args:          []string{"-c", "sleep 30"},
			Prompt:        "ignored",
			MaxConcurrent: 1,
		},
	})

	sc, _ := stageConfigs.Get("execute")
	session, err := spawnAgent(activeTicket, sc, s.Root, worktreeLayout(s.Config), stageConfigs, mon, runner, 0, 0)
	if err != nil {
		t.Fatalf("spawnAgent: %v", err)
	}
	if !runner.Alive(session) {
		t.Fatalf("session %q not alive", session)
	}

	handleCreate(s, stageConfigs, queuedPath, mon, runner)

	got, err := s.Get(queuedTicket.ID)
	if err != nil {
		t.Fatalf("Get queued: %v", err)
	}
	if got.QueuedAt.IsZero() {
		t.Fatal("QueuedAt is zero, want queue marker")
	}
	if _, err := agent.Latest(s.Root, queuedTicket.ID); err == nil {
		t.Fatal("queued ticket unexpectedly has an agent run")
	}

	if err := runner.Kill(session); err != nil {
		t.Fatalf("Kill(%q): %v", session, err)
	}
	waitForRunStatus(t, s.Root, activeTicket.ID, agent.StatusFailed)
}

func TestDrainQueuedStageStartsOldestQueuedTicketWhenCapacityFrees(t *testing.T) {
	s := newWatchStore(t)
	activeTicket, err := s.Create("Active")
	if err != nil {
		t.Fatalf("Create active: %v", err)
	}
	activeTicket, err = s.Move(activeTicket.ID, "execute")
	if err != nil {
		t.Fatalf("Move active: %v", err)
	}
	firstQueued, err := s.Create("First queued")
	if err != nil {
		t.Fatalf("Create first queued: %v", err)
	}
	firstQueued, err = s.Move(firstQueued.ID, "execute")
	if err != nil {
		t.Fatalf("Move first queued: %v", err)
	}
	secondQueued, err := s.Create("Second queued")
	if err != nil {
		t.Fatalf("Create second queued: %v", err)
	}
	secondQueued, err = s.Move(secondQueued.ID, "execute")
	if err != nil {
		t.Fatalf("Move second queued: %v", err)
	}

	runner := agent.NewPTYRunner()
	t.Cleanup(runner.Shutdown)
	mon := agent.NewMonitor(s.Root, 0, 0, 0, runner.Alive, runner.IdleSeconds, runner.Kill)
	stageConfigs := newStageConfigStore()
	stageConfigs.Set("execute", stage.Config{
		Agent: &stage.AgentConfig{
			Command:       "/bin/sh",
			Args:          []string{"-c", "sleep 30"},
			Prompt:        "ignored",
			MaxConcurrent: 1,
		},
	})
	sc, _ := stageConfigs.Get("execute")
	session, err := spawnAgent(activeTicket, sc, s.Root, worktreeLayout(s.Config), stageConfigs, mon, runner, 0, 0)
	if err != nil {
		t.Fatalf("spawnAgent: %v", err)
	}

	firstQueued.QueuedAt = time.Date(2026, time.April, 18, 9, 0, 0, 0, time.UTC)
	if err := s.Save(firstQueued); err != nil {
		t.Fatalf("Save first queued: %v", err)
	}
	secondQueued.QueuedAt = firstQueued.QueuedAt.Add(time.Minute)
	if err := s.Save(secondQueued); err != nil {
		t.Fatalf("Save second queued: %v", err)
	}

	if err := runner.Kill(session); err != nil {
		t.Fatalf("Kill(%q): %v", session, err)
	}
	waitForRunStatus(t, s.Root, activeTicket.ID, agent.StatusFailed)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		as, err := agent.Latest(s.Root, firstQueued.ID)
		if err == nil && as.RunID == "001-execute" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	firstRun, err := agent.Latest(s.Root, firstQueued.ID)
	if err != nil {
		t.Fatalf("Latest first queued: %v", err)
	}
	if firstRun.RunID != "001-execute" {
		t.Fatalf("first queued run id = %q, want 001-execute", firstRun.RunID)
	}
	refreshedFirst, err := s.Get(firstQueued.ID)
	if err != nil {
		t.Fatalf("Get first queued: %v", err)
	}
	if !refreshedFirst.QueuedAt.IsZero() {
		t.Fatalf("first queued QueuedAt = %v, want cleared", refreshedFirst.QueuedAt)
	}
	refreshedSecond, err := s.Get(secondQueued.ID)
	if err != nil {
		t.Fatalf("Get second queued: %v", err)
	}
	if refreshedSecond.QueuedAt.IsZero() {
		t.Fatal("second queued QueuedAt cleared unexpectedly")
	}

	if err := runner.Kill(firstRun.Session); err != nil {
		t.Fatalf("Kill(%q): %v", firstRun.Session, err)
	}
	waitForRunStatus(t, s.Root, firstQueued.ID, agent.StatusFailed)
}

func TestDrainQueuedStageOnStartupStartsQueuedTicket(t *testing.T) {
	s := newWatchStore(t)
	queuedTicket, err := s.Create("Queued")
	if err != nil {
		t.Fatalf("Create queued: %v", err)
	}
	queuedTicket, err = s.Move(queuedTicket.ID, "execute")
	if err != nil {
		t.Fatalf("Move queued: %v", err)
	}
	queuedTicket.QueuedAt = time.Date(2026, time.April, 18, 8, 0, 0, 0, time.UTC)
	if err := s.Save(queuedTicket); err != nil {
		t.Fatalf("Save queued: %v", err)
	}

	runner := agent.NewPTYRunner()
	t.Cleanup(runner.Shutdown)
	mon := agent.NewMonitor(s.Root, 0, 0, 0, runner.Alive, runner.IdleSeconds, runner.Kill)
	stageConfigs := newStageConfigStore()
	stageConfigs.Set("execute", stage.Config{
		Agent: &stage.AgentConfig{
			Command:       "/bin/sh",
			Args:          []string{"-c", "sleep 30"},
			Prompt:        "ignored",
			MaxConcurrent: 1,
		},
	})

	drainQueuedStage(s, stageConfigs, "execute", mon, runner)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		as, err := agent.Latest(s.Root, queuedTicket.ID)
		if err == nil && as.RunID == "001-execute" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	as, err := agent.Latest(s.Root, queuedTicket.ID)
	if err != nil {
		t.Fatalf("Latest queued: %v", err)
	}
	if as.RunID != "001-execute" {
		t.Fatalf("run id = %q, want 001-execute", as.RunID)
	}
	got, err := s.Get(queuedTicket.ID)
	if err != nil {
		t.Fatalf("Get queued: %v", err)
	}
	if !got.QueuedAt.IsZero() {
		t.Fatalf("QueuedAt = %v, want cleared", got.QueuedAt)
	}

	if err := runner.Kill(as.Session); err != nil {
		t.Fatalf("Kill(%q): %v", as.Session, err)
	}
	waitForRunStatus(t, s.Root, queuedTicket.ID, agent.StatusFailed)
}

func TestRerunStageAgentRefusesWhenStageAtCapacity(t *testing.T) {
	s := newWatchStore(t)
	activeTicket, err := s.Create("Active")
	if err != nil {
		t.Fatalf("Create active: %v", err)
	}
	activeTicket, err = s.Move(activeTicket.ID, "execute")
	if err != nil {
		t.Fatalf("Move active: %v", err)
	}
	blockedRerun, err := s.Create("Blocked rerun")
	if err != nil {
		t.Fatalf("Create blocked rerun: %v", err)
	}
	blockedRerun, err = s.Move(blockedRerun.ID, "execute")
	if err != nil {
		t.Fatalf("Move blocked rerun: %v", err)
	}

	runner := agent.NewPTYRunner()
	t.Cleanup(runner.Shutdown)
	mon := agent.NewMonitor(s.Root, 0, 0, 0, runner.Alive, runner.IdleSeconds, runner.Kill)
	stageConfigs := newStageConfigStore()
	stageConfigs.Set("execute", stage.Config{
		Agent: &stage.AgentConfig{
			Command:       "/bin/sh",
			Args:          []string{"-c", "sleep 30"},
			Prompt:        "ignored",
			MaxConcurrent: 1,
		},
	})
	sc, _ := stageConfigs.Get("execute")
	session, err := spawnAgent(activeTicket, sc, s.Root, worktreeLayout(s.Config), stageConfigs, mon, runner, 0, 0)
	if err != nil {
		t.Fatalf("spawnAgent: %v", err)
	}

	if _, err := rerunStageAgent(blockedRerun.ID, false, s, stageConfigs, mon, runner, 0, 0); !errors.Is(err, errStageAtCapacity) {
		t.Fatalf("rerunStageAgent error = %v, want stage capacity error", err)
	}
	if _, err := agent.Latest(s.Root, blockedRerun.ID); err == nil {
		t.Fatal("blocked rerun unexpectedly created an agent run")
	}

	if err := runner.Kill(session); err != nil {
		t.Fatalf("Kill(%q): %v", session, err)
	}
	waitForRunStatus(t, s.Root, activeTicket.ID, agent.StatusFailed)
}

func TestForceRerunDrainsSupersededOldStageQueue(t *testing.T) {
	s := newWatchStore(t)
	movedTicket, err := s.Create("Moved")
	if err != nil {
		t.Fatalf("Create moved: %v", err)
	}
	movedTicket, err = s.Move(movedTicket.ID, "execute")
	if err != nil {
		t.Fatalf("Move moved ticket: %v", err)
	}
	queuedTicket, err := s.Create("Queued")
	if err != nil {
		t.Fatalf("Create queued: %v", err)
	}
	queuedTicket, err = s.Move(queuedTicket.ID, "execute")
	if err != nil {
		t.Fatalf("Move queued: %v", err)
	}
	reviewTicket, err := s.Move(movedTicket.ID, "done")
	if err != nil {
		t.Fatalf("Move moved ticket to done: %v", err)
	}
	_ = reviewTicket

	runner := agent.NewPTYRunner()
	t.Cleanup(runner.Shutdown)
	mon := agent.NewMonitor(s.Root, 0, 0, 0, runner.Alive, runner.IdleSeconds, runner.Kill)
	stageConfigs := newStageConfigStore()
	stageConfigs.Set("execute", stage.Config{
		Agent: &stage.AgentConfig{
			Command:       "/bin/sh",
			Args:          []string{"-c", "sleep 30"},
			Prompt:        "ignored",
			MaxConcurrent: 1,
		},
	})
	stageConfigs.Set("done", stage.Config{
		Agent: &stage.AgentConfig{
			Command: "/bin/sh",
			Args:    []string{"-c", "sleep 30"},
			Prompt:  "ignored",
		},
	})
	executeConfig, _ := stageConfigs.Get("execute")
	session, err := spawnAgent(movedTicket, executeConfig, s.Root, worktreeLayout(s.Config), stageConfigs, mon, runner, 0, 0)
	if err != nil {
		t.Fatalf("spawnAgent execute: %v", err)
	}

	queuedTicket.QueuedAt = time.Date(2026, time.April, 18, 7, 0, 0, 0, time.UTC)
	if err := s.Save(queuedTicket); err != nil {
		t.Fatalf("Save queued: %v", err)
	}

	reloadedMoved, err := s.Get(movedTicket.ID)
	if err != nil {
		t.Fatalf("Get moved after done move: %v", err)
	}
	newSession, err := rerunStageAgent(reloadedMoved.ID, true, s, stageConfigs, mon, runner, 0, 0)
	if err != nil {
		t.Fatalf("rerunStageAgent(force): %v", err)
	}
	if newSession == session {
		t.Fatalf("force rerun reused old session %q", newSession)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		as, err := agent.Latest(s.Root, queuedTicket.ID)
		if err == nil && as.RunID == "001-execute" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	queuedRun, err := agent.Latest(s.Root, queuedTicket.ID)
	if err != nil {
		t.Fatalf("Latest queued: %v", err)
	}
	if queuedRun.RunID != "001-execute" {
		t.Fatalf("queued run id = %q, want 001-execute", queuedRun.RunID)
	}

	if err := runner.Kill(newSession); err != nil {
		t.Fatalf("Kill(%q): %v", newSession, err)
	}
	waitForRunStatus(t, s.Root, reloadedMoved.ID, agent.StatusFailed)
	if err := runner.Kill(queuedRun.Session); err != nil {
		t.Fatalf("Kill(%q): %v", queuedRun.Session, err)
	}
	waitForRunStatus(t, s.Root, queuedTicket.ID, agent.StatusFailed)
}

func TestMovedTicketActiveRunStillCountsAgainstOldStageCapacity(t *testing.T) {
	s := newWatchStore(t)
	movedTicket, err := s.Create("Moved")
	if err != nil {
		t.Fatalf("Create moved: %v", err)
	}
	movedTicket, err = s.Move(movedTicket.ID, "execute")
	if err != nil {
		t.Fatalf("Move moved: %v", err)
	}
	blockedTicket, err := s.Create("Blocked")
	if err != nil {
		t.Fatalf("Create blocked: %v", err)
	}
	blockedTicket, err = s.Move(blockedTicket.ID, "execute")
	if err != nil {
		t.Fatalf("Move blocked: %v", err)
	}

	runner := agent.NewPTYRunner()
	t.Cleanup(runner.Shutdown)
	mon := agent.NewMonitor(s.Root, 0, 0, 0, runner.Alive, runner.IdleSeconds, runner.Kill)
	stageConfigs := newStageConfigStore()
	stageConfigs.Set("execute", stage.Config{
		Agent: &stage.AgentConfig{
			Command:       "/bin/sh",
			Args:          []string{"-c", "sleep 30"},
			Prompt:        "ignored",
			MaxConcurrent: 1,
		},
	})
	stageConfigs.Set("done", stage.Config{})
	executeConfig, _ := stageConfigs.Get("execute")
	session, err := spawnAgent(movedTicket, executeConfig, s.Root, worktreeLayout(s.Config), stageConfigs, mon, runner, 0, 0)
	if err != nil {
		t.Fatalf("spawnAgent: %v", err)
	}

	if _, err := s.Move(movedTicket.ID, "done"); err != nil {
		t.Fatalf("Move moved ticket to done: %v", err)
	}
	if _, err := rerunStageAgent(blockedTicket.ID, false, s, stageConfigs, mon, runner, 0, 0); !errors.Is(err, errStageAtCapacity) {
		t.Fatalf("rerunStageAgent error = %v, want stage capacity error", err)
	}

	if err := runner.Kill(session); err != nil {
		t.Fatalf("Kill(%q): %v", session, err)
	}
	waitForRunStatus(t, s.Root, movedTicket.ID, agent.StatusFailed)
}

func TestRerunStageAgentClearsQueuedAt(t *testing.T) {
	s := newWatchStore(t)
	tk, err := s.Create("Queued rerun")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	tk, err = s.Move(tk.ID, "execute")
	if err != nil {
		t.Fatalf("Move: %v", err)
	}
	tk.QueuedAt = time.Date(2026, time.April, 18, 10, 0, 0, 0, time.UTC)
	if err := s.Save(tk); err != nil {
		t.Fatalf("Save: %v", err)
	}

	runner := agent.NewPTYRunner()
	t.Cleanup(runner.Shutdown)
	mon := agent.NewMonitor(s.Root, 0, 0, 0, runner.Alive, runner.IdleSeconds, runner.Kill)
	stageConfigs := newStageConfigStore()
	stageConfigs.Set("execute", stage.Config{
		Agent: &stage.AgentConfig{
			Command:       "/bin/sh",
			Args:          []string{"-c", "sleep 30"},
			Prompt:        "ignored",
			MaxConcurrent: 2,
		},
	})

	session, err := rerunStageAgent(tk.ID, false, s, stageConfigs, mon, runner, 0, 0)
	if err != nil {
		t.Fatalf("rerunStageAgent: %v", err)
	}

	got, err := s.Get(tk.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !got.QueuedAt.IsZero() {
		t.Fatalf("QueuedAt = %v, want cleared after rerun spawn", got.QueuedAt)
	}

	if err := runner.Kill(session); err != nil {
		t.Fatalf("Kill(%q): %v", session, err)
	}
	waitForRunStatus(t, s.Root, tk.ID, agent.StatusFailed)

	// After termination, drainQueuedStage runs via waitForSession. With
	// QueuedAt cleared, no phantom second run should appear.
	latest, err := agent.Latest(s.Root, tk.ID)
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if latest.RunID != "001-execute" {
		t.Fatalf("latest run id = %q, want 001-execute (no phantom rerun)", latest.RunID)
	}
}
