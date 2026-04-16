package ticket

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stepandel/tickets-md/internal/config"
)

// newTestStore creates a temporary store with three stages and returns
// the store along with a cleanup function.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	return newTestStoreWithConfig(t, config.Config{
		Prefix:        "T",
		ProjectPrefix: "PRJ",
		Stages:        []string{"backlog", "doing", "done"},
	})
}

func newTestStoreWithConfig(t *testing.T, c config.Config) *Store {
	t.Helper()
	root := t.TempDir()
	s, err := Init(root, c)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	return s
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func newGitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	runGit(t, root, "init", "-b", "main")
	return root
}

func newTestStoreWithCompleteStages(t *testing.T, completeStages ...string) *Store {
	t.Helper()
	return newTestStoreWithConfig(t, config.Config{
		Prefix:         "T",
		ProjectPrefix:  "PRJ",
		Stages:         []string{"backlog", "doing", "done"},
		CompleteStages: completeStages,
	})
}

func TestMoveIntoCompleteStageUnblocksDependents(t *testing.T) {
	s := newTestStoreWithCompleteStages(t, "done")
	blocker, _ := s.Create("Blocker")
	blocked, _ := s.Create("Blocked")

	if err := s.Link(blocked.ID, blocker.ID, "blocked_by"); err != nil {
		t.Fatalf("Link: %v", err)
	}

	moved, err := s.Move(blocker.ID, "done")
	if err != nil {
		t.Fatalf("Move: %v", err)
	}
	if len(moved.Blocks) != 0 {
		t.Fatalf("moved.Blocks = %v, want empty", moved.Blocks)
	}

	blocker, _ = s.Get(blocker.ID)
	blocked, _ = s.Get(blocked.ID)
	if len(blocker.Blocks) != 0 {
		t.Fatalf("blocker.Blocks = %v, want empty", blocker.Blocks)
	}
	if containsID(blocked.BlockedBy, blocker.ID) {
		t.Fatalf("blocked.BlockedBy = %v, want %s removed", blocked.BlockedBy, blocker.ID)
	}
}

func TestEnsureGitignored(t *testing.T) {
	t.Run("creates block in fresh repo", func(t *testing.T) {
		root := newGitRepo(t)

		if err := EnsureGitignored(root); err != nil {
			t.Fatalf("EnsureGitignored: %v", err)
		}
		if err := EnsureGitignored(root); err != nil {
			t.Fatalf("EnsureGitignored second: %v", err)
		}

		data, err := os.ReadFile(filepath.Join(root, ".gitignore"))
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
		if string(data) != gitignoreBlock+"\n" {
			t.Fatalf("gitignore = %q, want %q", string(data), gitignoreBlock+"\n")
		}
	})

	t.Run("replaces legacy tickets entry", func(t *testing.T) {
		root := newGitRepo(t)
		path := filepath.Join(root, ".gitignore")
		if err := os.WriteFile(path, []byte("node_modules\n.tickets\n.dist\n"), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		if err := EnsureGitignored(root); err != nil {
			t.Fatalf("EnsureGitignored: %v", err)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
		want := "node_modules\n" + gitignoreBlock + "\n.dist\n"
		if string(data) != want {
			t.Fatalf("gitignore = %q, want %q", string(data), want)
		}
	})

	t.Run("replaces legacy tickets entry with trailing slash", func(t *testing.T) {
		root := newGitRepo(t)
		path := filepath.Join(root, ".gitignore")
		if err := os.WriteFile(path, []byte("node_modules\n.tickets/\n.dist\n"), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		if err := EnsureGitignored(root); err != nil {
			t.Fatalf("EnsureGitignored: %v", err)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
		want := "node_modules\n" + gitignoreBlock + "\n.dist\n"
		if string(data) != want {
			t.Fatalf("gitignore = %q, want %q", string(data), want)
		}
	})

	t.Run("preserves unrelated entries when appending", func(t *testing.T) {
		root := newGitRepo(t)
		path := filepath.Join(root, ".gitignore")
		if err := os.WriteFile(path, []byte("node_modules\n"), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		if err := EnsureGitignored(root); err != nil {
			t.Fatalf("EnsureGitignored: %v", err)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
		want := "node_modules\n" + gitignoreBlock + "\n"
		if string(data) != want {
			t.Fatalf("gitignore = %q, want %q", string(data), want)
		}
	})

	t.Run("git respects stage config exception", func(t *testing.T) {
		root := newGitRepo(t)
		if err := EnsureGitignored(root); err != nil {
			t.Fatalf("EnsureGitignored: %v", err)
		}
		if err := os.MkdirAll(filepath.Join(root, ".tickets", "execute"), 0o755); err != nil {
			t.Fatalf("MkdirAll execute: %v", err)
		}
		if err := os.WriteFile(filepath.Join(root, ".tickets", "execute", ".stage.yml"), []byte("agent: {}\n"), 0o644); err != nil {
			t.Fatalf("WriteFile stage config: %v", err)
		}
		if err := os.WriteFile(filepath.Join(root, ".tickets", "execute", "TIC-001.md"), []byte("# test\n"), 0o644); err != nil {
			t.Fatalf("WriteFile ticket: %v", err)
		}

		stageCheck := exec.Command("git", "check-ignore", ".tickets/execute/.stage.yml")
		stageCheck.Dir = root
		if out, err := stageCheck.CombinedOutput(); err == nil {
			t.Fatalf("stage config should not be ignored, got %q", strings.TrimSpace(string(out)))
		}

		ticketCheck := exec.Command("git", "check-ignore", ".tickets/execute/TIC-001.md")
		ticketCheck.Dir = root
		out, err := ticketCheck.CombinedOutput()
		if err != nil {
			t.Fatalf("ticket markdown should be ignored: %v\n%s", err, out)
		}
		if strings.TrimSpace(string(out)) != ".tickets/execute/TIC-001.md" {
			t.Fatalf("check-ignore output = %q", strings.TrimSpace(string(out)))
		}
	})
}

func TestMoveIntoNonCompleteStagePreservesBlocks(t *testing.T) {
	s := newTestStoreWithCompleteStages(t, "done")
	blocker, _ := s.Create("Blocker")
	blocked, _ := s.Create("Blocked")

	if err := s.Link(blocked.ID, blocker.ID, "blocked_by"); err != nil {
		t.Fatalf("Link: %v", err)
	}

	if _, err := s.Move(blocker.ID, "doing"); err != nil {
		t.Fatalf("Move: %v", err)
	}

	blocker, _ = s.Get(blocker.ID)
	blocked, _ = s.Get(blocked.ID)
	if !containsID(blocker.Blocks, blocked.ID) {
		t.Fatalf("blocker.Blocks = %v, want %s present", blocker.Blocks, blocked.ID)
	}
	if !containsID(blocked.BlockedBy, blocker.ID) {
		t.Fatalf("blocked.BlockedBy = %v, want %s present", blocked.BlockedBy, blocker.ID)
	}
}

func TestMoveIntoCompleteStageWithoutBlocksIsOrdinaryMove(t *testing.T) {
	s := newTestStoreWithCompleteStages(t, "done")
	tk, _ := s.Create("Solo")

	moved, err := s.Move(tk.ID, "done")
	if err != nil {
		t.Fatalf("Move: %v", err)
	}
	if moved.Stage != "done" {
		t.Fatalf("Stage = %q, want done", moved.Stage)
	}
	if len(moved.Blocks) != 0 {
		t.Fatalf("Blocks = %v, want empty", moved.Blocks)
	}
}

func TestMoveSameStageDoesNotUnblock(t *testing.T) {
	s := newTestStoreWithCompleteStages(t, "done")
	blocker, _ := s.Create("Blocker")
	blocked, _ := s.Create("Blocked")

	if err := s.Link(blocked.ID, blocker.ID, "blocked_by"); err != nil {
		t.Fatalf("Link: %v", err)
	}
	if _, err := s.Move(blocker.ID, "backlog"); err != nil {
		t.Fatalf("Move: %v", err)
	}

	blocker, _ = s.Get(blocker.ID)
	blocked, _ = s.Get(blocked.ID)
	if !containsID(blocker.Blocks, blocked.ID) {
		t.Fatalf("blocker.Blocks = %v, want %s present", blocker.Blocks, blocked.ID)
	}
	if !containsID(blocked.BlockedBy, blocker.ID) {
		t.Fatalf("blocked.BlockedBy = %v, want %s present", blocked.BlockedBy, blocker.ID)
	}
}

func TestMoveIntoCompleteStageWithDanglingBlockedPeerStillSucceeds(t *testing.T) {
	s := newTestStoreWithCompleteStages(t, "done")
	blocker, _ := s.Create("Blocker")
	blocked, _ := s.Create("Blocked")

	if err := s.Link(blocked.ID, blocker.ID, "blocked_by"); err != nil {
		t.Fatalf("Link: %v", err)
	}
	if err := s.Delete(blocked.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	blocker, err := s.Get(blocker.ID)
	if err != nil {
		t.Fatalf("Get blocker: %v", err)
	}
	blocker.Blocks = appendID(blocker.Blocks, blocked.ID)
	if err := s.Save(blocker); err != nil {
		t.Fatalf("Save blocker with dangling block: %v", err)
	}

	moved, err := s.Move(blocker.ID, "done")
	if err != nil {
		t.Fatalf("Move: %v", err)
	}
	if len(moved.Blocks) != 0 {
		t.Fatalf("moved.Blocks = %v, want empty", moved.Blocks)
	}

	blocker, err = s.Get(blocker.ID)
	if err != nil {
		t.Fatalf("Get blocker after move: %v", err)
	}
	if len(blocker.Blocks) != 0 {
		t.Fatalf("blocker.Blocks = %v, want empty", blocker.Blocks)
	}
}

func TestMoveIntoCompleteStageClearsAsymmetricBlockedResidue(t *testing.T) {
	s := newTestStoreWithCompleteStages(t, "done")
	blocker, _ := s.Create("Blocker")
	blocked, _ := s.Create("Blocked")

	blocked.BlockedBy = []string{blocker.ID}
	if err := s.Save(blocker); err != nil {
		t.Fatalf("Save blocker: %v", err)
	}
	if err := s.Save(blocked); err != nil {
		t.Fatalf("Save blocked: %v", err)
	}

	moved, err := s.Move(blocker.ID, "done")
	if err != nil {
		t.Fatalf("Move: %v", err)
	}
	if len(moved.Blocks) != 0 {
		t.Fatalf("moved.Blocks = %v, want empty", moved.Blocks)
	}

	blocked, err = s.Get(blocked.ID)
	if err != nil {
		t.Fatalf("Get blocked: %v", err)
	}
	if containsID(blocked.BlockedBy, blocker.ID) {
		t.Fatalf("blocked.BlockedBy = %v, want %s removed", blocked.BlockedBy, blocker.ID)
	}
}

func TestLinkRelated(t *testing.T) {
	s := newTestStore(t)
	a, err := s.Create("Alpha")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	b, err := s.Create("Beta")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := s.Link(a.ID, b.ID, "related"); err != nil {
		t.Fatalf("Link: %v", err)
	}

	// Reload and verify both sides.
	a, _ = s.Get(a.ID)
	b, _ = s.Get(b.ID)

	if !containsID(a.Related, b.ID) {
		t.Errorf("expected %s in %s.Related, got %v", b.ID, a.ID, a.Related)
	}
	if !containsID(b.Related, a.ID) {
		t.Errorf("expected %s in %s.Related, got %v", a.ID, b.ID, b.Related)
	}
}

func TestLinkBlockedBy(t *testing.T) {
	s := newTestStore(t)
	a, _ := s.Create("Alpha")
	b, _ := s.Create("Beta")

	// a is blocked by b
	if err := s.Link(a.ID, b.ID, "blocked_by"); err != nil {
		t.Fatalf("Link: %v", err)
	}

	a, _ = s.Get(a.ID)
	b, _ = s.Get(b.ID)

	if !containsID(a.BlockedBy, b.ID) {
		t.Errorf("expected %s in %s.BlockedBy, got %v", b.ID, a.ID, a.BlockedBy)
	}
	if !containsID(b.Blocks, a.ID) {
		t.Errorf("expected %s in %s.Blocks, got %v", a.ID, b.ID, b.Blocks)
	}
}

func TestLinkParent(t *testing.T) {
	s := newTestStore(t)
	child, _ := s.Create("Child")
	parent, _ := s.Create("Parent")

	if err := s.Link(child.ID, parent.ID, "parent"); err != nil {
		t.Fatalf("Link: %v", err)
	}

	child, _ = s.Get(child.ID)
	parent, _ = s.Get(parent.ID)

	if child.Parent != parent.ID {
		t.Errorf("expected %s.Parent=%s, got %q", child.ID, parent.ID, child.Parent)
	}
	if !containsID(parent.Children, child.ID) {
		t.Errorf("expected %s in %s.Children, got %v", child.ID, parent.ID, parent.Children)
	}
}

func TestLinkSelfRejected(t *testing.T) {
	s := newTestStore(t)
	a, _ := s.Create("Alpha")

	if err := s.Link(a.ID, a.ID, "related"); err == nil {
		t.Fatal("expected error for self-link, got nil")
	}
}

func TestLinkDuplicateRejected(t *testing.T) {
	s := newTestStore(t)
	a, _ := s.Create("Alpha")
	b, _ := s.Create("Beta")

	if err := s.Link(a.ID, b.ID, "related"); err != nil {
		t.Fatalf("Link: %v", err)
	}
	if err := s.Link(a.ID, b.ID, "related"); err == nil {
		t.Fatal("expected error for duplicate link, got nil")
	}
}

func TestLinkParentRejectedWhenChildAlreadyHasParent(t *testing.T) {
	s := newTestStore(t)
	child, _ := s.Create("Child")
	parentA, _ := s.Create("Parent A")
	parentB, _ := s.Create("Parent B")

	if err := s.Link(child.ID, parentA.ID, "parent"); err != nil {
		t.Fatalf("initial Link: %v", err)
	}
	if err := s.Link(child.ID, parentB.ID, "parent"); err == nil {
		t.Fatal("expected error when child already has a parent")
	}
}

func TestLinkParentCycleRejected(t *testing.T) {
	s := newTestStore(t)
	a, _ := s.Create("Alpha")
	b, _ := s.Create("Beta")

	if err := s.Link(b.ID, a.ID, "parent"); err != nil {
		t.Fatalf("Link: %v", err)
	}
	if err := s.Link(a.ID, b.ID, "parent"); err == nil {
		t.Fatal("expected cycle rejection, got nil")
	}
}

func TestUnlinkRelated(t *testing.T) {
	s := newTestStore(t)
	a, _ := s.Create("Alpha")
	b, _ := s.Create("Beta")
	s.Link(a.ID, b.ID, "related")

	if err := s.Unlink(a.ID, b.ID, "related"); err != nil {
		t.Fatalf("Unlink: %v", err)
	}

	a, _ = s.Get(a.ID)
	b, _ = s.Get(b.ID)

	if len(a.Related) != 0 {
		t.Errorf("expected empty Related on %s, got %v", a.ID, a.Related)
	}
	if len(b.Related) != 0 {
		t.Errorf("expected empty Related on %s, got %v", b.ID, b.Related)
	}
}

func TestUnlinkParent(t *testing.T) {
	s := newTestStore(t)
	child, _ := s.Create("Child")
	parent, _ := s.Create("Parent")
	if err := s.Link(child.ID, parent.ID, "parent"); err != nil {
		t.Fatalf("Link: %v", err)
	}

	if err := s.Unlink(child.ID, parent.ID, "parent"); err != nil {
		t.Fatalf("Unlink: %v", err)
	}

	child, _ = s.Get(child.ID)
	parent, _ = s.Get(parent.ID)

	if child.Parent != "" {
		t.Errorf("expected empty Parent on %s, got %q", child.ID, child.Parent)
	}
	if len(parent.Children) != 0 {
		t.Errorf("expected empty Children on %s, got %v", parent.ID, parent.Children)
	}
}

func TestDeleteCleansUpLinks(t *testing.T) {
	s := newTestStore(t)
	a, _ := s.Create("Alpha")
	b, _ := s.Create("Beta")
	c, _ := s.Create("Gamma")

	s.Link(a.ID, b.ID, "related")
	s.Link(a.ID, c.ID, "blocked_by") // a blocked by c

	// Delete a — b and c should have their links cleaned up.
	if err := s.Delete(a.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// a should be gone.
	if _, err := s.Get(a.ID); err == nil {
		t.Fatal("expected ErrNotFound after delete")
	}

	b, _ = s.Get(b.ID)
	c, _ = s.Get(c.ID)

	if containsID(b.Related, a.ID) {
		t.Errorf("expected %s removed from %s.Related, got %v", a.ID, b.ID, b.Related)
	}
	if containsID(c.Blocks, a.ID) {
		t.Errorf("expected %s removed from %s.Blocks, got %v", a.ID, c.ID, c.Blocks)
	}
}

func TestDeleteParentOrphansChildren(t *testing.T) {
	s := newTestStore(t)
	parent, _ := s.Create("Parent")
	child, _ := s.Create("Child")

	if err := s.Link(child.ID, parent.ID, "parent"); err != nil {
		t.Fatalf("Link: %v", err)
	}
	if err := s.Delete(parent.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	child, _ = s.Get(child.ID)
	if child.Parent != "" {
		t.Errorf("expected child parent cleared after deleting parent, got %q", child.Parent)
	}
}

func TestDeleteChildRemovesFromParentChildren(t *testing.T) {
	s := newTestStore(t)
	parent, _ := s.Create("Parent")
	child, _ := s.Create("Child")

	if err := s.Link(child.ID, parent.ID, "parent"); err != nil {
		t.Fatalf("Link: %v", err)
	}
	if err := s.Delete(child.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	parent, _ = s.Get(parent.ID)
	if containsID(parent.Children, child.ID) {
		t.Errorf("expected %s removed from parent children, got %v", child.ID, parent.Children)
	}
}

func TestLinkNonExistentTarget(t *testing.T) {
	s := newTestStore(t)
	a, _ := s.Create("Alpha")

	if err := s.Link(a.ID, "T-999", "related"); err == nil {
		t.Fatal("expected error for non-existent target, got nil")
	}
}

func TestExistingTicketsWithoutLinksUnchanged(t *testing.T) {
	s := newTestStore(t)
	a, _ := s.Create("Alpha")

	// Read the file, marshal, and verify no link fields appear.
	data, err := os.ReadFile(a.Path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(data)

	// Ensure no link-related YAML keys appear in the frontmatter.
	for _, key := range []string{"related:", "blocked_by:", "blocks:"} {
		if strings.Contains(content, key) {
			t.Errorf("expected no %q in frontmatter of ticket without links", key)
		}
	}
	for _, key := range []string{"parent:", "children:"} {
		if strings.Contains(content, key) {
			t.Errorf("expected no %q in frontmatter of ticket without links", key)
		}
	}
}

func TestTicketHasLinksAndLinkCount(t *testing.T) {
	tk := Ticket{
		Related:   []string{"T-001"},
		BlockedBy: []string{"T-002", "T-003"},
		Parent:    "T-004",
		Children:  []string{"T-005"},
	}
	if !tk.HasLinks() {
		t.Error("expected HasLinks() to be true")
	}
	if tk.LinkCount() != 5 {
		t.Errorf("expected LinkCount() == 3, got %d", tk.LinkCount())
	}
}

func TestCreateAndSetPriority(t *testing.T) {
	s := newTestStore(t)
	a, err := s.Create("Alpha")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	a.Priority = "high"
	if err := s.Save(a); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := s.Get(a.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Priority != "high" {
		t.Errorf("expected Priority=high, got %q", got.Priority)
	}
}

func TestPriorityOmittedWhenEmpty(t *testing.T) {
	s := newTestStore(t)
	a, err := s.Create("Alpha")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	data, err := os.ReadFile(a.Path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.Contains(string(data), "priority:") {
		t.Errorf("expected no priority key in frontmatter for ticket without priority, got:\n%s", string(data))
	}
}

func TestPriorityClearedOnSave(t *testing.T) {
	s := newTestStore(t)
	a, err := s.Create("Alpha")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	a.Priority = "low"
	if err := s.Save(a); err != nil {
		t.Fatalf("Save: %v", err)
	}

	a, _ = s.Get(a.ID)
	a.Priority = ""
	if err := s.Save(a); err != nil {
		t.Fatalf("Save clear: %v", err)
	}

	data, err := os.ReadFile(a.Path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.Contains(string(data), "priority:") {
		t.Errorf("expected priority key absent after clear, got:\n%s", string(data))
	}

	got, _ := s.Get(a.ID)
	if got.Priority != "" {
		t.Errorf("expected empty priority, got %q", got.Priority)
	}
}

func TestProjectRoundTripsThroughSaveLoad(t *testing.T) {
	s := newTestStore(t)
	p, err := s.CreateProject("Spring launch")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	a, err := s.Create("Alpha")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	a.Project = p.ID
	if err := s.Save(a); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := s.Get(a.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Project != p.ID {
		t.Fatalf("Project = %q, want %q", got.Project, p.ID)
	}
}

func TestLinksText(t *testing.T) {
	tk := Ticket{
		Related:   []string{"T-001"},
		BlockedBy: []string{"T-002"},
		Blocks:    []string{"T-003"},
		Parent:    "T-004",
		Children:  []string{"T-005"},
	}
	text := tk.LinksText()
	if text == "" {
		t.Fatal("expected non-empty LinksText")
	}
	for _, want := range []string{"parent: T-004", "children: T-005"} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected %q in LinksText, got %q", want, text)
		}
	}
}
