package ticket

import (
	"os"
	"strings"
	"testing"

	"tickets-md/internal/config"
)

// newTestStore creates a temporary store with three stages and returns
// the store along with a cleanup function.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	root := t.TempDir()
	c := config.Config{
		Prefix: "T",
		Stages: []string{"backlog", "doing", "done"},
	}
	s, err := Init(root, c)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	return s
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
}

func TestTicketHasLinksAndLinkCount(t *testing.T) {
	tk := Ticket{
		Related:   []string{"T-001"},
		BlockedBy: []string{"T-002", "T-003"},
	}
	if !tk.HasLinks() {
		t.Error("expected HasLinks() to be true")
	}
	if tk.LinkCount() != 3 {
		t.Errorf("expected LinkCount() == 3, got %d", tk.LinkCount())
	}
}

func TestLinksText(t *testing.T) {
	tk := Ticket{
		Related:   []string{"T-001"},
		BlockedBy: []string{"T-002"},
		Blocks:    []string{"T-003"},
	}
	text := tk.LinksText()
	if text == "" {
		t.Fatal("expected non-empty LinksText")
	}
}

