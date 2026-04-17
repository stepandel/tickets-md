package cli

import (
	"context"
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
	}, s.Root, layout, mon, runner, 0, 0)
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
	}, s.Root, layout, mon, runner, 0, 0)
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
	}, s.Root, layout, mon, runner, 0, 0)
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
	}, s.Root, layout, mon, runner, 0, 0)
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

	handleCreate(s, map[string]stage.Config{
		"backlog": {},
		"execute": {},
		"done":    {},
	}, dst, nil, nil)

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

	stageConfigs := map[string]stage.Config{
		"execute": {
			Agent: &stage.AgentConfig{
				Command: "/bin/sh",
				Args:    []string{"-c", "sleep 30"},
				Prompt:  "ignored",
			},
		},
	}

	session, err := spawnAgent(tk, stageConfigs["execute"], s.Root, layout, mon, runner, 0, 0)
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

	stageConfigs := map[string]stage.Config{
		"execute": {
			Agent: &stage.AgentConfig{
				Command: "/bin/sh",
				Args:    []string{"-c", "sleep 30"},
				Prompt:  "ignored",
			},
		},
	}

	firstSession, err := spawnAgent(tk, stageConfigs["execute"], s.Root, layout, mon, runner, 0, 0)
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
