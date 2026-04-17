package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/stepandel/tickets-md/internal/agent"
	"github.com/stepandel/tickets-md/internal/config"
	"github.com/stepandel/tickets-md/internal/ticket"
)

func newArchiveStore(t *testing.T) *ticket.Store {
	t.Helper()
	root := t.TempDir()
	s, err := ticket.Init(root, config.Config{
		Prefix:        "TIC",
		ProjectPrefix: "PRJ",
		Stages:        []string{"backlog", "done", "archive"},
		ArchiveStage:  "archive",
	})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	return s
}

func TestArchiveCommandMovesSingleTicket(t *testing.T) {
	s := newArchiveStore(t)
	tk, err := s.Create("Done ticket")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := s.Move(tk.ID, "done"); err != nil {
		t.Fatalf("Move: %v", err)
	}

	globalFlags.root = s.Root
	cmd := newArchiveCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{tk.ID})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	got, err := s.Get(tk.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Stage != "archive" {
		t.Fatalf("Stage = %q, want archive", got.Stage)
	}
	if !strings.Contains(out.String(), "Archived "+tk.ID+" -> archive") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestArchiveCommandRequiresArchiveStage(t *testing.T) {
	s := newCLITestStore(t)
	tk, err := s.Create("Ticket")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	globalFlags.root = s.Root
	cmd := newArchiveCmd()
	cmd.SetArgs([]string{tk.ID})
	err = cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "archive_stage is not set") {
		t.Fatalf("Execute() error = %v, want archive_stage is not set", err)
	}
}

func TestArchiveCommandBulkOlderThan(t *testing.T) {
	s := newArchiveStore(t)
	oldTicket, err := s.Create("Older ticket")
	if err != nil {
		t.Fatalf("Create old ticket: %v", err)
	}
	if _, err := s.Move(oldTicket.ID, "done"); err != nil {
		t.Fatalf("Move old ticket: %v", err)
	}
	recentTicket, err := s.Create("Recent ticket")
	if err != nil {
		t.Fatalf("Create recent ticket: %v", err)
	}
	if _, err := s.Move(recentTicket.ID, "done"); err != nil {
		t.Fatalf("Move recent ticket: %v", err)
	}

	oldTk, err := s.Get(oldTicket.ID)
	if err != nil {
		t.Fatalf("Get old ticket: %v", err)
	}
	oldTk.UpdatedAt = time.Now().UTC().Add(-31 * 24 * time.Hour).Truncate(time.Second)
	if err := oldTk.WriteFile(); err != nil {
		t.Fatalf("WriteFile old ticket: %v", err)
	}

	recentTk, err := s.Get(recentTicket.ID)
	if err != nil {
		t.Fatalf("Get recent ticket: %v", err)
	}
	recentTk.UpdatedAt = time.Now().UTC().Add(-6 * time.Hour).Truncate(time.Second)
	if err := recentTk.WriteFile(); err != nil {
		t.Fatalf("WriteFile recent ticket: %v", err)
	}

	globalFlags.root = s.Root
	cmd := newArchiveCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--from", "done", "--older-than", "720h"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	oldTk, err = s.Get(oldTicket.ID)
	if err != nil {
		t.Fatalf("Get old ticket: %v", err)
	}
	if oldTk.Stage != "archive" {
		t.Fatalf("old ticket stage = %q, want archive", oldTk.Stage)
	}
	recentTk, err = s.Get(recentTicket.ID)
	if err != nil {
		t.Fatalf("Get recent ticket: %v", err)
	}
	if recentTk.Stage != "done" {
		t.Fatalf("recent ticket stage = %q, want done", recentTk.Stage)
	}
	if !strings.Contains(out.String(), "1 ticket(s) archived") {
		t.Fatalf("output = %q, want summary", out.String())
	}
}

func TestArchiveCommandBulkRefusesArchiveStageSource(t *testing.T) {
	s := newArchiveStore(t)

	globalFlags.root = s.Root
	cmd := newArchiveCmd()
	cmd.SetArgs([]string{"--from", "archive", "--older-than", "720h"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "cannot bulk archive from archive stage") {
		t.Fatalf("Execute() error = %v, want archive stage refusal", err)
	}
}

func TestArchiveCommandBulkSkipsActiveRuns(t *testing.T) {
	s := newArchiveStore(t)
	tk, err := s.Create("Active ticket")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := s.Move(tk.ID, "done"); err != nil {
		t.Fatalf("Move: %v", err)
	}
	tk, err = s.Get(tk.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	tk.UpdatedAt = time.Now().UTC().Add(-31 * 24 * time.Hour).Truncate(time.Second)
	if err := tk.WriteFile(); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := agent.Write(s.Root, agent.AgentStatus{
		TicketID:  tk.ID,
		RunID:     "001-done",
		Seq:       1,
		Attempt:   1,
		Stage:     "done",
		Agent:     "claude",
		Status:    agent.StatusRunning,
		SpawnedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("agent.Write: %v", err)
	}

	globalFlags.root = s.Root
	cmd := newArchiveCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--from", "done", "--older-than", "720h"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	got, err := s.Get(tk.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Stage != "done" {
		t.Fatalf("Stage = %q, want done", got.Stage)
	}
	if !strings.Contains(out.String(), "skipped with active agent runs") {
		t.Fatalf("output = %q, want active-run summary", out.String())
	}
}
