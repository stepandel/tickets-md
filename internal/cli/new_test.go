package cli

import (
	"errors"
	"slices"
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

func TestNewCommandWithProject(t *testing.T) {
	s := newCLITestStore(t)
	project, err := s.CreateProject("Project")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	globalFlags.root = s.Root
	cmd := newNewCmd()
	cmd.SetArgs([]string{"--project", project.ID, "Scoped work"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	created, err := s.Get("TIC-001")
	if err != nil {
		t.Fatalf("Get created: %v", err)
	}
	if created.Project != project.ID {
		t.Fatalf("expected project %s, got %q", project.ID, created.Project)
	}
}

func TestNewCommandWithUnknownProjectFails(t *testing.T) {
	s := newCLITestStore(t)

	globalFlags.root = s.Root
	cmd := newNewCmd()
	cmd.SetArgs([]string{"--project", "PRJ-999", "Scoped work"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ticket.ErrProjectNotFound) {
		t.Fatalf("expected project not found, got %v", err)
	}

	created, getErr := s.Get("TIC-001")
	if getErr != nil {
		t.Fatalf("Get created after failure: %v", getErr)
	}
	if created.Project != "" {
		t.Fatalf("expected project to remain empty, got %q", created.Project)
	}
}

func assertNoTicketCreated(t *testing.T, s *ticket.Store) {
	t.Helper()
	if _, err := s.Get("TIC-001"); !errors.Is(err, ticket.ErrNotFound) {
		t.Fatalf("expected TIC-001 to be absent, got %v", err)
	}
	nextID, err := s.NextID()
	if err != nil {
		t.Fatalf("NextID: %v", err)
	}
	if nextID != "TIC-001" {
		t.Fatalf("expected next ID TIC-001, got %s", nextID)
	}
}

func TestNewCommandWithUnknownParentFailsBeforeCreate(t *testing.T) {
	s := newCLITestStore(t)

	globalFlags.root = s.Root
	cmd := newNewCmd()
	cmd.SetArgs([]string{"--parent", "TIC-999", "Child"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ticket.ErrNotFound) {
		t.Fatalf("expected ticket not found, got %v", err)
	}

	assertNoTicketCreated(t, s)
}

func TestNewCommandWithUnknownBlockedByFailsBeforeCreate(t *testing.T) {
	s := newCLITestStore(t)

	globalFlags.root = s.Root
	cmd := newNewCmd()
	cmd.SetArgs([]string{"--blocked-by", "TIC-999", "Blocked work"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ticket.ErrNotFound) {
		t.Fatalf("expected ticket not found, got %v", err)
	}

	assertNoTicketCreated(t, s)
}

func TestNewCommandWithUnknownBlocksFailsBeforeCreate(t *testing.T) {
	s := newCLITestStore(t)

	globalFlags.root = s.Root
	cmd := newNewCmd()
	cmd.SetArgs([]string{"--blocks", "TIC-999", "Blocker"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ticket.ErrNotFound) {
		t.Fatalf("expected ticket not found, got %v", err)
	}

	assertNoTicketCreated(t, s)
}

func TestNewCommandWithUnknownRelatedFailsBeforeCreate(t *testing.T) {
	s := newCLITestStore(t)

	globalFlags.root = s.Root
	cmd := newNewCmd()
	cmd.SetArgs([]string{"--related", "TIC-999", "Related work"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ticket.ErrNotFound) {
		t.Fatalf("expected ticket not found, got %v", err)
	}

	assertNoTicketCreated(t, s)
}

func TestNewCommandRejectsEmptyRelationIDBeforeCreate(t *testing.T) {
	s := newCLITestStore(t)

	globalFlags.root = s.Root
	cmd := newNewCmd()
	cmd.SetArgs([]string{"--blocked-by", "   ", "Blocked work"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "requires a non-empty ticket ID") {
		t.Fatalf("expected non-empty ticket ID error, got %v", err)
	}

	assertNoTicketCreated(t, s)
}

func TestNewCommandRejectsDuplicateRelationIDsBeforeCreate(t *testing.T) {
	s := newCLITestStore(t)
	peer, err := s.Create("Peer")
	if err != nil {
		t.Fatalf("Create peer: %v", err)
	}

	globalFlags.root = s.Root
	cmd := newNewCmd()
	cmd.SetArgs([]string{"--blocked-by", peer.ID, "--blocked-by", " " + peer.ID + " ", "Blocked work"})
	err = cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "duplicate ticket ID") {
		t.Fatalf("expected duplicate relation error, got %v", err)
	}

	nextID, nextErr := s.NextID()
	if nextErr != nil {
		t.Fatalf("NextID: %v", nextErr)
	}
	if nextID != "TIC-002" {
		t.Fatalf("expected next ID TIC-002, got %s", nextID)
	}
	if _, getErr := s.Get("TIC-002"); !errors.Is(getErr, ticket.ErrNotFound) {
		t.Fatalf("expected TIC-002 to be absent, got %v", getErr)
	}
}

func TestNewCommandRejectsConflictingRelationRolesBeforeCreate(t *testing.T) {
	s := newCLITestStore(t)
	peer, err := s.Create("Peer")
	if err != nil {
		t.Fatalf("Create peer: %v", err)
	}

	globalFlags.root = s.Root
	cmd := newNewCmd()
	cmd.SetArgs([]string{"--blocked-by", peer.ID, "--blocks", " " + peer.ID + " ", "Blocked work"})
	err = cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "cannot be used with both") {
		t.Fatalf("expected conflicting relation error, got %v", err)
	}

	nextID, nextErr := s.NextID()
	if nextErr != nil {
		t.Fatalf("NextID: %v", nextErr)
	}
	if nextID != "TIC-002" {
		t.Fatalf("expected next ID TIC-002, got %s", nextID)
	}
	if _, getErr := s.Get("TIC-002"); !errors.Is(getErr, ticket.ErrNotFound) {
		t.Fatalf("expected TIC-002 to be absent, got %v", getErr)
	}
}

func TestNewCommandWithBlockedBy(t *testing.T) {
	s := newCLITestStore(t)
	blocker, err := s.Create("Blocker")
	if err != nil {
		t.Fatalf("Create blocker: %v", err)
	}

	globalFlags.root = s.Root
	cmd := newNewCmd()
	cmd.SetArgs([]string{"--blocked-by", blocker.ID, "Blocked work"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	created, err := s.Get("TIC-002")
	if err != nil {
		t.Fatalf("Get created: %v", err)
	}
	if !slices.Contains(created.BlockedBy, blocker.ID) {
		t.Fatalf("expected blocked_by to contain %s, got %v", blocker.ID, created.BlockedBy)
	}

	blocker, err = s.Get(blocker.ID)
	if err != nil {
		t.Fatalf("Get blocker: %v", err)
	}
	if !slices.Contains(blocker.Blocks, created.ID) {
		t.Fatalf("expected blocker blocks to contain %s, got %v", created.ID, blocker.Blocks)
	}
}

func TestNewCommandWithBlocks(t *testing.T) {
	s := newCLITestStore(t)
	blocked, err := s.Create("Blocked")
	if err != nil {
		t.Fatalf("Create blocked: %v", err)
	}

	globalFlags.root = s.Root
	cmd := newNewCmd()
	cmd.SetArgs([]string{"--blocks", blocked.ID, "Blocker"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	created, err := s.Get("TIC-002")
	if err != nil {
		t.Fatalf("Get created: %v", err)
	}
	if !slices.Contains(created.Blocks, blocked.ID) {
		t.Fatalf("expected blocks to contain %s, got %v", blocked.ID, created.Blocks)
	}

	blocked, err = s.Get(blocked.ID)
	if err != nil {
		t.Fatalf("Get blocked: %v", err)
	}
	if !slices.Contains(blocked.BlockedBy, created.ID) {
		t.Fatalf("expected blocked_by to contain %s, got %v", created.ID, blocked.BlockedBy)
	}
}

func TestNewCommandWithRelated(t *testing.T) {
	s := newCLITestStore(t)
	peer, err := s.Create("Peer")
	if err != nil {
		t.Fatalf("Create peer: %v", err)
	}

	globalFlags.root = s.Root
	cmd := newNewCmd()
	cmd.SetArgs([]string{"--related", peer.ID, "Related work"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	created, err := s.Get("TIC-002")
	if err != nil {
		t.Fatalf("Get created: %v", err)
	}
	if !slices.Contains(created.Related, peer.ID) {
		t.Fatalf("expected related to contain %s, got %v", peer.ID, created.Related)
	}

	peer, err = s.Get(peer.ID)
	if err != nil {
		t.Fatalf("Get peer: %v", err)
	}
	if !slices.Contains(peer.Related, created.ID) {
		t.Fatalf("expected peer related to contain %s, got %v", created.ID, peer.Related)
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

func TestNewCommandCombinedFlags(t *testing.T) {
	s := newCLITestStore(t)
	project, err := s.CreateProject("Project")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	parent, err := s.Create("Parent")
	if err != nil {
		t.Fatalf("Create parent: %v", err)
	}
	blocker, err := s.Create("Blocker")
	if err != nil {
		t.Fatalf("Create blocker: %v", err)
	}
	blocked, err := s.Create("Blocked")
	if err != nil {
		t.Fatalf("Create blocked: %v", err)
	}
	peer, err := s.Create("Peer")
	if err != nil {
		t.Fatalf("Create peer: %v", err)
	}

	globalFlags.root = s.Root
	cmd := newNewCmd()
	cmd.SetArgs([]string{
		"--body", "## Description\n\nDetailed body.",
		"--priority", "high",
		"--project", project.ID,
		"--parent", parent.ID,
		"--blocked-by", blocker.ID,
		"--blocks", blocked.ID,
		"--related", peer.ID,
		"Child",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	child, err := s.Get("TIC-005")
	if err != nil {
		t.Fatalf("Get child: %v", err)
	}
	if strings.TrimLeft(child.Body, "\n") != "## Description\n\nDetailed body.\n" {
		t.Fatalf("expected child body to be preserved, got %q", child.Body)
	}
	if child.Priority != "high" {
		t.Fatalf("expected child priority high, got %q", child.Priority)
	}
	if child.Project != project.ID {
		t.Fatalf("expected child project %s, got %q", project.ID, child.Project)
	}
	if child.Parent != parent.ID {
		t.Fatalf("expected child parent %s, got %q", parent.ID, child.Parent)
	}
	if !slices.Contains(child.BlockedBy, blocker.ID) {
		t.Fatalf("expected blocked_by to contain %s, got %v", blocker.ID, child.BlockedBy)
	}
	if !slices.Contains(child.Blocks, blocked.ID) {
		t.Fatalf("expected blocks to contain %s, got %v", blocked.ID, child.Blocks)
	}
	if !slices.Contains(child.Related, peer.ID) {
		t.Fatalf("expected related to contain %s, got %v", peer.ID, child.Related)
	}

	parent, err = s.Get(parent.ID)
	if err != nil {
		t.Fatalf("Get parent: %v", err)
	}
	if !slices.Contains(parent.Children, child.ID) {
		t.Fatalf("expected parent children to contain %s, got %v", child.ID, parent.Children)
	}

	blocker, err = s.Get(blocker.ID)
	if err != nil {
		t.Fatalf("Get blocker: %v", err)
	}
	if !slices.Contains(blocker.Blocks, child.ID) {
		t.Fatalf("expected blocker blocks to contain %s, got %v", child.ID, blocker.Blocks)
	}

	blocked, err = s.Get(blocked.ID)
	if err != nil {
		t.Fatalf("Get blocked: %v", err)
	}
	if !slices.Contains(blocked.BlockedBy, child.ID) {
		t.Fatalf("expected blocked_by to contain %s, got %v", child.ID, blocked.BlockedBy)
	}

	peer, err = s.Get(peer.ID)
	if err != nil {
		t.Fatalf("Get peer: %v", err)
	}
	if !slices.Contains(peer.Related, child.ID) {
		t.Fatalf("expected peer related to contain %s, got %v", child.ID, peer.Related)
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
