package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStripAnsi(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plain text", "hello world", "hello world"},
		{"color codes", "\x1b[31mred\x1b[0m", "red"},
		{"cursor movement", "\x1b[2Jcleared", "cleared"},
		{"osc sequence", "\x1b]0;title\x07text", "text"},
		{"mixed", "\x1b[1;32mbold green\x1b[0m normal", "bold green normal"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripAnsi(tt.input)
			if got != tt.want {
				t.Errorf("StripAnsi(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestTailLines(t *testing.T) {
	input := "line1\nline2\nline3\nline4\nline5"

	t.Run("within limit", func(t *testing.T) {
		got := tailLines(input, 10)
		if got != input {
			t.Errorf("expected full input, got %q", got)
		}
	})

	t.Run("truncated", func(t *testing.T) {
		got := tailLines(input, 3)
		if !strings.Contains(got, "line3") || !strings.Contains(got, "line4") || !strings.Contains(got, "line5") {
			t.Errorf("expected last 3 lines, got %q", got)
		}
		if !strings.Contains(got, "truncated") {
			t.Errorf("expected truncation note, got %q", got)
		}
		if strings.Contains(got, "line1") || strings.Contains(got, "line2") {
			t.Errorf("should not contain early lines, got %q", got)
		}
	})
}

func TestTruncateLines(t *testing.T) {
	input := "a\nb\nc\nd\ne"

	t.Run("within limit", func(t *testing.T) {
		got := truncateLines(input, 10, "note")
		if got != input {
			t.Errorf("expected full input, got %q", got)
		}
	})

	t.Run("truncated", func(t *testing.T) {
		got := truncateLines(input, 3, "(cut)")
		if !strings.HasPrefix(got, "a\nb\nc\n") {
			t.Errorf("expected first 3 lines, got %q", got)
		}
		if !strings.HasSuffix(got, "(cut)") {
			t.Errorf("expected truncation note, got %q", got)
		}
	})
}

func TestExtractBody(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			"with frontmatter",
			"---\nid: TIC-001\ntitle: Test\n---\n\n## Description\nSome body here.",
			"## Description\nSome body here.",
		},
		{
			"no frontmatter",
			"Just plain text",
			"Just plain text",
		},
		{
			"empty body",
			"---\nid: TIC-002\n---\n",
			"",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractBody(tt.content)
			if got != tt.want {
				t.Errorf("extractBody() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGatherContext(t *testing.T) {
	root := t.TempDir()
	ticketID := "TIC-001"

	// Set up a log file with ANSI codes.
	runsDir := filepath.Join(root, ".tickets", ".agents", ticketID, "runs")
	if err := os.MkdirAll(runsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	logContent := "\x1b[32mAgent output line 1\x1b[0m\nLine 2\nLine 3\n"
	logFile := filepath.Join(runsDir, "001-execute.log")
	if err := os.WriteFile(logFile, []byte(logContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Set up a ticket file.
	ticketPath := filepath.Join(root, "ticket.md")
	ticketContent := "---\nid: TIC-001\ntitle: Test Ticket\n---\n\n## Description\nDo the thing."
	if err := os.WriteFile(ticketPath, []byte(ticketContent), 0o644); err != nil {
		t.Fatal(err)
	}

	run := AgentStatus{
		TicketID: ticketID,
		RunID:    "001-execute",
		LogFile:  logFile,
		// No worktree — diff should be empty.
	}

	ctx, err := GatherContext(root, run, ticketPath, 200, 8000)
	if err != nil {
		t.Fatal(err)
	}

	// Diff should be empty (no worktree).
	if ctx.Diff != "" {
		t.Errorf("expected empty diff, got %q", ctx.Diff)
	}

	// Log should be stripped of ANSI and contain the output.
	if !strings.Contains(ctx.Log, "Agent output line 1") {
		t.Errorf("expected log to contain stripped text, got %q", ctx.Log)
	}
	if strings.Contains(ctx.Log, "\x1b") {
		t.Errorf("expected ANSI codes to be stripped, got %q", ctx.Log)
	}

	// Ticket body should be extracted.
	if !strings.Contains(ctx.Ticket, "Do the thing") {
		t.Errorf("expected ticket body, got %q", ctx.Ticket)
	}
	if strings.Contains(ctx.Ticket, "id: TIC-001") {
		t.Errorf("expected frontmatter to be stripped, got %q", ctx.Ticket)
	}
}

func TestGatherContextMissingLog(t *testing.T) {
	root := t.TempDir()

	run := AgentStatus{
		TicketID: "TIC-002",
		RunID:    "001-execute",
		LogFile:  filepath.Join(root, "nonexistent.log"),
	}

	ctx, err := GatherContext(root, run, "", 200, 8000)
	if err != nil {
		t.Fatal(err)
	}
	if ctx.Log != "" {
		t.Errorf("expected empty log for missing file, got %q", ctx.Log)
	}
}

func TestGatherContextMissingWorktree(t *testing.T) {
	root := t.TempDir()

	run := AgentStatus{
		TicketID: "TIC-003",
		RunID:    "001-execute",
		Worktree: filepath.Join(root, "nonexistent-worktree"),
	}

	ctx, err := GatherContext(root, run, "", 200, 8000)
	if err != nil {
		t.Fatal(err)
	}
	if ctx.Diff != "" {
		t.Errorf("expected empty diff for missing worktree, got %q", ctx.Diff)
	}
}
