package ticket

import (
	"testing"
)

func TestDoctorClean(t *testing.T) {
	s := newTestStore(t)
	a, _ := s.Create("Alpha")
	b, _ := s.Create("Beta")
	s.Link(a.ID, b.ID, "related")

	issues, err := s.Doctor(false)
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}
	if len(issues) != 0 {
		t.Errorf("expected 0 issues, got %d: %v", len(issues), issues)
	}
}

func TestDoctorDanglingRelated(t *testing.T) {
	s := newTestStore(t)
	a, _ := s.Create("Alpha")

	// Manually add a dangling related ref.
	a.Related = []string{"T-999"}
	s.Save(a)

	issues, err := s.Doctor(false)
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d: %v", len(issues), issues)
	}
	if issues[0].Kind != Dangling {
		t.Errorf("expected Dangling, got %v", issues[0].Kind)
	}
	if issues[0].Field != FieldRelated {
		t.Errorf("expected FieldRelated, got %v", issues[0].Field)
	}
	if !issues[0].Fixed {
		t.Error("expected issue to be fixed")
	}

	// Verify the dangling ref was removed.
	a, _ = s.Get(a.ID)
	if containsID(a.Related, "T-999") {
		t.Error("expected T-999 removed from Related")
	}
}

func TestDoctorOneSidedRelated(t *testing.T) {
	s := newTestStore(t)
	a, _ := s.Create("Alpha")
	b, _ := s.Create("Beta")

	// Manually set one side of a related link.
	a.Related = []string{b.ID}
	s.Save(a)

	issues, err := s.Doctor(false)
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d: %v", len(issues), issues)
	}
	if issues[0].Kind != OneSided {
		t.Errorf("expected OneSided, got %v", issues[0].Kind)
	}
	if !issues[0].Fixed {
		t.Error("expected issue to be fixed")
	}

	// Verify reciprocal was added.
	b, _ = s.Get(b.ID)
	if !containsID(b.Related, a.ID) {
		t.Errorf("expected %s in %s.Related", a.ID, b.ID)
	}
}

func TestDoctorDanglingBlockedBy(t *testing.T) {
	s := newTestStore(t)
	a, _ := s.Create("Alpha")

	a.BlockedBy = []string{"T-999"}
	s.Save(a)

	issues, err := s.Doctor(false)
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d: %v", len(issues), issues)
	}
	if issues[0].Kind != Dangling {
		t.Errorf("expected Dangling, got %v", issues[0].Kind)
	}
	if issues[0].Field != FieldBlockedBy {
		t.Errorf("expected FieldBlockedBy, got %v", issues[0].Field)
	}

	a, _ = s.Get(a.ID)
	if containsID(a.BlockedBy, "T-999") {
		t.Error("expected T-999 removed from BlockedBy")
	}
}

func TestDoctorOneSidedBlockedBy(t *testing.T) {
	s := newTestStore(t)
	a, _ := s.Create("Alpha")
	b, _ := s.Create("Beta")

	// A is blocked by B, but B does not list A in Blocks.
	a.BlockedBy = []string{b.ID}
	s.Save(a)

	issues, err := s.Doctor(false)
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d: %v", len(issues), issues)
	}
	if issues[0].Kind != OneSided {
		t.Errorf("expected OneSided, got %v", issues[0].Kind)
	}

	b, _ = s.Get(b.ID)
	if !containsID(b.Blocks, a.ID) {
		t.Errorf("expected %s in %s.Blocks", a.ID, b.ID)
	}
}

func TestDoctorOneSidedBlocks(t *testing.T) {
	s := newTestStore(t)
	a, _ := s.Create("Alpha")
	b, _ := s.Create("Beta")

	// A blocks B, but B does not list A in BlockedBy.
	a.Blocks = []string{b.ID}
	s.Save(a)

	issues, err := s.Doctor(false)
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d: %v", len(issues), issues)
	}
	if issues[0].Kind != OneSided {
		t.Errorf("expected OneSided, got %v", issues[0].Kind)
	}

	b, _ = s.Get(b.ID)
	if !containsID(b.BlockedBy, a.ID) {
		t.Errorf("expected %s in %s.BlockedBy", a.ID, b.ID)
	}
}

func TestDoctorDanglingParent(t *testing.T) {
	s := newTestStore(t)
	child, _ := s.Create("Child")

	child.Parent = "T-999"
	s.Save(child)

	issues, err := s.Doctor(false)
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d: %v", len(issues), issues)
	}
	if issues[0].Kind != Dangling || issues[0].Field != FieldParent {
		t.Fatalf("expected dangling parent issue, got %+v", issues[0])
	}

	child, _ = s.Get(child.ID)
	if child.Parent != "" {
		t.Fatalf("expected dangling parent cleared, got %q", child.Parent)
	}
}

func TestDoctorOneSidedParent(t *testing.T) {
	s := newTestStore(t)
	child, _ := s.Create("Child")
	parent, _ := s.Create("Parent")

	child.Parent = parent.ID
	s.Save(child)

	issues, err := s.Doctor(false)
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d: %v", len(issues), issues)
	}
	if issues[0].Kind != OneSided || issues[0].Field != FieldParent {
		t.Fatalf("expected one-sided parent issue, got %+v", issues[0])
	}

	parent, _ = s.Get(parent.ID)
	if !containsID(parent.Children, child.ID) {
		t.Fatalf("expected %s added to parent children, got %v", child.ID, parent.Children)
	}
}

func TestDoctorDanglingChild(t *testing.T) {
	s := newTestStore(t)
	parent, _ := s.Create("Parent")

	parent.Children = []string{"T-999"}
	s.Save(parent)

	issues, err := s.Doctor(false)
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d: %v", len(issues), issues)
	}
	if issues[0].Kind != Dangling || issues[0].Field != FieldChildren {
		t.Fatalf("expected dangling child issue, got %+v", issues[0])
	}

	parent, _ = s.Get(parent.ID)
	if containsID(parent.Children, "T-999") {
		t.Fatalf("expected dangling child removed, got %v", parent.Children)
	}
}

func TestDoctorOneSidedChild(t *testing.T) {
	s := newTestStore(t)
	parent, _ := s.Create("Parent")
	child, _ := s.Create("Child")

	parent.Children = []string{child.ID}
	s.Save(parent)

	issues, err := s.Doctor(false)
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d: %v", len(issues), issues)
	}
	if issues[0].Kind != OneSided || issues[0].Field != FieldChildren {
		t.Fatalf("expected one-sided child issue, got %+v", issues[0])
	}

	child, _ = s.Get(child.ID)
	if child.Parent != parent.ID {
		t.Fatalf("expected child parent set to %s, got %q", parent.ID, child.Parent)
	}
}

func TestDoctorChildConflictDropsInvalidChildRef(t *testing.T) {
	s := newTestStore(t)
	parentA, _ := s.Create("Parent A")
	parentB, _ := s.Create("Parent B")
	child, _ := s.Create("Child")

	parentA.Children = []string{child.ID}
	child.Parent = parentB.ID
	s.Save(parentA)
	s.Save(child)

	issues, err := s.Doctor(false)
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("expected 2 issues, got %d: %v", len(issues), issues)
	}
	var sawConflict, sawRepair bool
	for _, issue := range issues {
		if issue.Kind == Dangling && issue.Field == FieldChildren {
			sawConflict = true
		}
		if issue.Kind == OneSided && issue.Field == FieldParent {
			sawRepair = true
		}
	}
	if !sawConflict || !sawRepair {
		t.Fatalf("expected dangling child conflict and parent repair, got %v", issues)
	}

	parentA, _ = s.Get(parentA.ID)
	if containsID(parentA.Children, child.ID) {
		t.Fatalf("expected conflicting child removed from parent, got %v", parentA.Children)
	}
	child, _ = s.Get(child.ID)
	if child.Parent != parentB.ID {
		t.Fatalf("expected child's existing parent preserved, got %q", child.Parent)
	}
}

func TestDoctorDryRun(t *testing.T) {
	s := newTestStore(t)
	a, _ := s.Create("Alpha")

	a.Related = []string{"T-999"}
	s.Save(a)

	issues, err := s.Doctor(true)
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
	if issues[0].Fixed {
		t.Error("expected issue NOT to be fixed in dry-run mode")
	}

	// Verify nothing changed on disk.
	a, _ = s.Get(a.ID)
	if !containsID(a.Related, "T-999") {
		t.Error("expected T-999 still in Related after dry-run")
	}
}

func TestDoctorMultipleIssues(t *testing.T) {
	s := newTestStore(t)
	a, _ := s.Create("Alpha")
	b, _ := s.Create("Beta")
	c, _ := s.Create("Gamma")

	// Dangling related on A.
	a.Related = []string{"T-999"}
	s.Save(a)

	// One-sided blocked_by on B (B blocked by C, but C doesn't list B in Blocks).
	b.BlockedBy = []string{c.ID}
	s.Save(b)

	issues, err := s.Doctor(false)
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("expected 2 issues, got %d: %v", len(issues), issues)
	}

	// Verify fixes applied.
	a, _ = s.Get(a.ID)
	if containsID(a.Related, "T-999") {
		t.Error("expected T-999 removed from A.Related")
	}
	c, _ = s.Get(c.ID)
	if !containsID(c.Blocks, b.ID) {
		t.Errorf("expected %s in %s.Blocks", b.ID, c.ID)
	}
}

func TestDoctorTicketTargeted(t *testing.T) {
	s := newTestStore(t)
	a, _ := s.Create("Alpha")
	b, _ := s.Create("Beta")

	// One-sided: A related to B, but B doesn't know.
	a.Related = []string{b.ID}
	s.Save(a)

	issues, err := s.DoctorTicket(a.ID, false)
	if err != nil {
		t.Fatalf("DoctorTicket: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d: %v", len(issues), issues)
	}
	if issues[0].Kind != OneSided {
		t.Errorf("expected OneSided, got %v", issues[0].Kind)
	}
	if !issues[0].Fixed {
		t.Error("expected issue to be fixed")
	}

	b, _ = s.Get(b.ID)
	if !containsID(b.Related, a.ID) {
		t.Errorf("expected %s in %s.Related", a.ID, b.ID)
	}
}

func TestDoctorTicketDangling(t *testing.T) {
	s := newTestStore(t)
	a, _ := s.Create("Alpha")

	a.Blocks = []string{"T-999"}
	s.Save(a)

	issues, err := s.DoctorTicket(a.ID, false)
	if err != nil {
		t.Fatalf("DoctorTicket: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d: %v", len(issues), issues)
	}
	if issues[0].Kind != Dangling {
		t.Errorf("expected Dangling, got %v", issues[0].Kind)
	}

	a, _ = s.Get(a.ID)
	if containsID(a.Blocks, "T-999") {
		t.Error("expected T-999 removed from Blocks")
	}
}

func TestDoctorTicketSelfParent(t *testing.T) {
	s := newTestStore(t)
	child, _ := s.Create("Child")

	child.Parent = child.ID
	s.Save(child)

	issues, err := s.DoctorTicket(child.ID, false)
	if err != nil {
		t.Fatalf("DoctorTicket: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d: %v", len(issues), issues)
	}
	if issues[0].Kind != Dangling || issues[0].Field != FieldParent {
		t.Fatalf("expected dangling self-parent issue, got %+v", issues[0])
	}

	child, _ = s.Get(child.ID)
	if child.Parent != "" {
		t.Fatalf("expected self-parent cleared, got %q", child.Parent)
	}
}

func TestDoctorMultipleDanglingsSameSlice(t *testing.T) {
	s := newTestStore(t)
	a, _ := s.Create("Alpha")
	b, _ := s.Create("Beta")

	// A.Related has a valid ref sandwiched between two dangling refs.
	// This exercises the range-while-mutating path: removeID shifts the
	// backing array, so a naïve range over the live slice skips elements.
	a.Related = []string{"T-997", b.ID, "T-999"}
	s.Save(a)

	issues, err := s.Doctor(false)
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}

	// Expect: 2 dangling (T-997, T-999) + 1 one-sided (b missing reciprocal).
	danglings := 0
	oneSided := 0
	for _, iss := range issues {
		switch iss.Kind {
		case Dangling:
			danglings++
		case OneSided:
			oneSided++
		}
	}
	if danglings != 2 {
		t.Errorf("expected 2 dangling issues, got %d: %v", danglings, issues)
	}
	if oneSided != 1 {
		t.Errorf("expected 1 one-sided issue, got %d: %v", oneSided, issues)
	}

	// Verify fixes: dangling refs removed, reciprocal added on B.
	a, _ = s.Get(a.ID)
	if containsID(a.Related, "T-997") || containsID(a.Related, "T-999") {
		t.Errorf("expected dangling refs removed, got Related=%v", a.Related)
	}
	if !containsID(a.Related, b.ID) {
		t.Errorf("expected %s still in A.Related", b.ID)
	}
	b, _ = s.Get(b.ID)
	if !containsID(b.Related, a.ID) {
		t.Errorf("expected %s in B.Related (reciprocal)", a.ID)
	}
}

func TestDoctorSelfLink(t *testing.T) {
	s := newTestStore(t)
	a, _ := s.Create("Alpha")

	a.Related = []string{a.ID}
	s.Save(a)

	issues, err := s.Doctor(false)
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d: %v", len(issues), issues)
	}
	if issues[0].Kind != Dangling {
		t.Errorf("expected Dangling for self-link, got %v", issues[0].Kind)
	}

	a, _ = s.Get(a.ID)
	if containsID(a.Related, a.ID) {
		t.Error("expected self-link removed")
	}
}
