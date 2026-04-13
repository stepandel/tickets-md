package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tickets-md/internal/agent"
)

func TestComposeFollowupPrompt(t *testing.T) {
	ctx := agent.RunContext{
		Diff:   "--- a/main.go\n+++ b/main.go\n@@ -1 +1 @@\n-old\n+new",
		Log:    "Agent did some work\nFinished successfully",
		Ticket: "## Description\nDo the thing.",
	}

	t.Run("with message", func(t *testing.T) {
		got := composeFollowupPrompt("TIC-001", "Test Ticket", "/path/to/ticket.md", "/path/to/worktree", ctx, "also add tests")
		if !strings.Contains(got, "TIC-001") {
			t.Error("expected ticket ID in prompt")
		}
		if !strings.Contains(got, "Test Ticket") {
			t.Error("expected title in prompt")
		}
		if !strings.Contains(got, "```diff") {
			t.Error("expected diff block in prompt")
		}
		if !strings.Contains(got, "also add tests") {
			t.Error("expected follow-up message in prompt")
		}
		if !strings.Contains(got, "/path/to/worktree") {
			t.Error("expected worktree path in prompt")
		}
	})

	t.Run("without message", func(t *testing.T) {
		got := composeFollowupPrompt("TIC-001", "Test Ticket", "/path/to/ticket.md", "", ctx, "")
		if strings.Contains(got, "## Follow-up") {
			t.Error("should not contain follow-up section when message is empty")
		}
		if strings.Contains(got, "worktree") {
			t.Error("should not mention worktree when empty")
		}
	})

	t.Run("without diff", func(t *testing.T) {
		noDiff := agent.RunContext{Log: "some output"}
		got := composeFollowupPrompt("TIC-002", "No Diff", "/path.md", "", noDiff, "fix it")
		if strings.Contains(got, "```diff") {
			t.Error("should not contain diff block when diff is empty")
		}
		if !strings.Contains(got, "some output") {
			t.Error("expected log output in prompt")
		}
	})
}

func TestLatestTerminalRun(t *testing.T) {
	root := t.TempDir()
	ticketID := "TIC-TEST"

	dir := filepath.Join(root, ".tickets", ".agents", ticketID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Write a running run and a done run.
	running := agent.AgentStatus{
		TicketID: ticketID,
		RunID:    "001-execute",
		Seq:      1,
		Status:   agent.StatusSpawned,
	}
	if err := agent.Write(root, running); err != nil {
		t.Fatal(err)
	}

	done := agent.AgentStatus{
		TicketID: ticketID,
		RunID:    "002-execute",
		Seq:      2,
		Status:   agent.StatusSpawned,
	}
	if err := agent.Write(root, done); err != nil {
		t.Fatal(err)
	}
	// Transition to done.
	done.Status = agent.StatusRunning
	if err := agent.Write(root, done); err != nil {
		t.Fatal(err)
	}
	done.Status = agent.StatusDone
	exitCode := 0
	done.ExitCode = &exitCode
	if err := agent.Write(root, done); err != nil {
		t.Fatal(err)
	}

	t.Run("finds latest terminal", func(t *testing.T) {
		got, err := latestTerminalRun(root, ticketID)
		if err != nil {
			t.Fatal(err)
		}
		if got.RunID != "002-execute" {
			t.Errorf("expected 002-execute, got %s", got.RunID)
		}
		if got.Status != agent.StatusDone {
			t.Errorf("expected done, got %s", got.Status)
		}
	})

	t.Run("no terminal runs", func(t *testing.T) {
		otherRoot := t.TempDir()
		otherID := "TIC-NONE"
		otherDir := filepath.Join(otherRoot, ".tickets", ".agents", otherID)
		if err := os.MkdirAll(otherDir, 0o755); err != nil {
			t.Fatal(err)
		}
		spawned := agent.AgentStatus{
			TicketID: otherID,
			RunID:    "001-execute",
			Seq:      1,
			Status:   agent.StatusSpawned,
		}
		if err := agent.Write(otherRoot, spawned); err != nil {
			t.Fatal(err)
		}
		_, err := latestTerminalRun(otherRoot, otherID)
		if err == nil {
			t.Error("expected error for no terminal runs")
		}
	})
}
