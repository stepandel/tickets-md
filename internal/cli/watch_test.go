package cli

import (
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
		Prefix: "TIC",
		Stages: []string{"backlog", "execute", "done"},
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
	mon := agent.NewMonitor(s.Root, runner.Alive, runner.IdleSeconds)
	mon.OnStatusChange = func(ticketID string) {
		syncAgentFrontmatter(s.Root, ticketID)
	}

	_, err = spawnAgent(tk, stage.Config{
		Agent: &stage.AgentConfig{
			Command: "/definitely/missing/tickets-agent",
			Prompt:  "ignored",
		},
	}, s.Root, mon, runner, 0, 0)
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
	mon := agent.NewMonitor(s.Root, runner.Alive, runner.IdleSeconds)
	mon.OnStatusChange = func(ticketID string) {
		syncAgentFrontmatter(s.Root, ticketID)
	}

	session, err := spawnAgent(tk, stage.Config{
		Agent: &stage.AgentConfig{
			Command: "/bin/sh",
			Args:    []string{"-c", "exit 127"},
			Prompt:  "ignored",
		},
	}, s.Root, mon, runner, 0, 0)
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
