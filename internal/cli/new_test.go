package cli

import (
	"strings"
	"testing"

	"github.com/stepandel/tickets-md/internal/config"
	"github.com/stepandel/tickets-md/internal/ticket"
)

func newCLITestStore(t *testing.T) *ticket.Store {
	t.Helper()
	root := t.TempDir()
	s, err := ticket.Init(root, config.Config{
		Prefix:        "TIC",
		ProjectPrefix: "PRJ",
		Stages:        []string{"backlog", "execute", "done"},
	})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	return s
}

func TestNewCommandWithParent(t *testing.T) {
	s := newCLITestStore(t)
	parent, err := s.Create("Parent")
	if err != nil {
		t.Fatalf("Create parent: %v", err)
	}

	globalFlags.root = s.Root
	cmd := newNewCmd()
	cmd.SetArgs([]string{"--parent", parent.ID, "Child"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	child, err := s.Get("TIC-002")
	if err != nil {
		t.Fatalf("Get child: %v", err)
	}
	if child.Parent != parent.ID {
		t.Fatalf("expected child parent %s, got %q", parent.ID, child.Parent)
	}

	parent, err = s.Get(parent.ID)
	if err != nil {
		t.Fatalf("Get parent: %v", err)
	}
	if len(parent.Children) != 1 || parent.Children[0] != child.ID {
		t.Fatalf("expected parent children [%s], got %v", child.ID, parent.Children)
	}
}

func TestNewCommandWithBody(t *testing.T) {
	s := newCLITestStore(t)

	globalFlags.root = s.Root
	cmd := newNewCmd()
	cmd.SetArgs([]string{"--body", "## Description\n\nDetailed body.", "Body title"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	created, err := s.Get("TIC-001")
	if err != nil {
		t.Fatalf("Get created: %v", err)
	}
	if strings.TrimLeft(created.Body, "\n") != "## Description\n\nDetailed body.\n" {
		t.Fatalf("expected custom body, got %q", created.Body)
	}
}

func TestNewCommandWithParentAndPriority(t *testing.T) {
	s := newCLITestStore(t)
	parent, err := s.Create("Parent")
	if err != nil {
		t.Fatalf("Create parent: %v", err)
	}

	globalFlags.root = s.Root
	cmd := newNewCmd()
	cmd.SetArgs([]string{"--parent", parent.ID, "--priority", "high", "Child"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	child, err := s.Get("TIC-002")
	if err != nil {
		t.Fatalf("Get child: %v", err)
	}
	if child.Parent != parent.ID {
		t.Fatalf("expected child parent %s, got %q", parent.ID, child.Parent)
	}
	if child.Priority != "high" {
		t.Fatalf("expected child priority high, got %q", child.Priority)
	}

	parent, err = s.Get(parent.ID)
	if err != nil {
		t.Fatalf("Get parent: %v", err)
	}
	if len(parent.Children) != 1 || parent.Children[0] != child.ID {
		t.Fatalf("expected parent children [%s], got %v", child.ID, parent.Children)
	}
}

func TestNewCommandWithBodyParentAndPriority(t *testing.T) {
	s := newCLITestStore(t)
	parent, err := s.Create("Parent")
	if err != nil {
		t.Fatalf("Create parent: %v", err)
	}

	globalFlags.root = s.Root
	cmd := newNewCmd()
	cmd.SetArgs([]string{"--body", "## Description\n\nDetailed body.", "--parent", parent.ID, "--priority", "high", "Child"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	child, err := s.Get("TIC-002")
	if err != nil {
		t.Fatalf("Get child: %v", err)
	}
	if strings.TrimLeft(child.Body, "\n") != "## Description\n\nDetailed body.\n" {
		t.Fatalf("expected child body to be preserved, got %q", child.Body)
	}
	if child.Parent != parent.ID {
		t.Fatalf("expected child parent %s, got %q", parent.ID, child.Parent)
	}
	if child.Priority != "high" {
		t.Fatalf("expected child priority high, got %q", child.Priority)
	}

	parent, err = s.Get(parent.ID)
	if err != nil {
		t.Fatalf("Get parent: %v", err)
	}
	if len(parent.Children) != 1 || parent.Children[0] != child.ID {
		t.Fatalf("expected parent children [%s], got %v", child.ID, parent.Children)
	}
}

func TestLinkCommandRejectsConflictingRelationFlags(t *testing.T) {
	s := newCLITestStore(t)
	a, _ := s.Create("Alpha")
	b, _ := s.Create("Beta")

	globalFlags.root = s.Root
	cmd := newLinkCmd()
	cmd.SetArgs([]string{"--blocks", "--parent", a.ID, b.ID})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when both --blocks and --parent are set")
	}
}
