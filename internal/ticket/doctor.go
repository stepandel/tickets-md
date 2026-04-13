package ticket

import (
	"fmt"
	"sort"
)

// IssueKind classifies a link problem found by Doctor.
type IssueKind int

const (
	// Dangling means the target ticket does not exist.
	Dangling IssueKind = iota
	// OneSided means the reciprocal link is missing on the peer.
	OneSided
)

func (k IssueKind) String() string {
	switch k {
	case Dangling:
		return "dangling"
	case OneSided:
		return "one-sided"
	default:
		return "unknown"
	}
}

// LinkField identifies which link slice contains the problem.
type LinkField int

const (
	FieldRelated   LinkField = iota
	FieldBlockedBy
	FieldBlocks
)

func (f LinkField) String() string {
	switch f {
	case FieldRelated:
		return "related"
	case FieldBlockedBy:
		return "blocked_by"
	case FieldBlocks:
		return "blocks"
	default:
		return "unknown"
	}
}

// Issue describes a single link integrity problem found by Doctor.
type Issue struct {
	Kind     IssueKind
	Field    LinkField
	TicketID string
	TargetID string
	Fixed    bool
}

// String returns a human-readable description of the issue.
func (i Issue) String() string {
	action := "found"
	if i.Fixed {
		switch i.Kind {
		case Dangling:
			action = "removed"
		case OneSided:
			action = "added reciprocal"
		}
	}
	return fmt.Sprintf("[doctor] %s: %s %s ref %s — %s", i.TicketID, i.Kind, i.Field, i.TargetID, action)
}

// Doctor scans every ticket in the store for broken links and
// optionally repairs them. It returns a list of issues found.
//
// When dryRun is true, issues are reported but nothing is modified on
// disk.
func (s *Store) Doctor(dryRun bool) ([]Issue, error) {
	all, err := s.ListAll()
	if err != nil {
		return nil, err
	}

	// Build a flat map of pointers so mutations propagate.
	tickets := make(map[string]*Ticket)
	for stage, list := range all {
		for i := range list {
			list[i].Stage = stage
			tickets[list[i].ID] = &list[i]
		}
	}

	// Sorted iteration for deterministic output.
	ids := make([]string, 0, len(tickets))
	for id := range tickets {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	modified := make(map[string]bool)
	var issues []Issue

	for _, id := range ids {
		t := tickets[id]

		// --- Related (symmetric) ---
		// Snapshot the slice: removeID reuses the backing array, so
		// ranging over the live slice while removing skips elements.
		for _, ref := range append([]string(nil), t.Related...) {
			if ref == id {
				// Self-link — treat as dangling.
				issues = append(issues, Issue{Kind: Dangling, Field: FieldRelated, TicketID: id, TargetID: ref})
				if !dryRun {
					t.Related = removeID(t.Related, ref)
					modified[id] = true
				}
				continue
			}
			peer, ok := tickets[ref]
			if !ok {
				issues = append(issues, Issue{Kind: Dangling, Field: FieldRelated, TicketID: id, TargetID: ref})
				if !dryRun {
					t.Related = removeID(t.Related, ref)
					modified[id] = true
				}
				continue
			}
			if !containsID(peer.Related, id) {
				issues = append(issues, Issue{Kind: OneSided, Field: FieldRelated, TicketID: id, TargetID: ref})
				if !dryRun {
					peer.Related = appendID(peer.Related, id)
					modified[ref] = true
				}
			}
		}

		// --- BlockedBy → peer.Blocks ---
		for _, ref := range append([]string(nil), t.BlockedBy...) {
			if ref == id {
				issues = append(issues, Issue{Kind: Dangling, Field: FieldBlockedBy, TicketID: id, TargetID: ref})
				if !dryRun {
					t.BlockedBy = removeID(t.BlockedBy, ref)
					modified[id] = true
				}
				continue
			}
			peer, ok := tickets[ref]
			if !ok {
				issues = append(issues, Issue{Kind: Dangling, Field: FieldBlockedBy, TicketID: id, TargetID: ref})
				if !dryRun {
					t.BlockedBy = removeID(t.BlockedBy, ref)
					modified[id] = true
				}
				continue
			}
			if !containsID(peer.Blocks, id) {
				issues = append(issues, Issue{Kind: OneSided, Field: FieldBlockedBy, TicketID: id, TargetID: ref})
				if !dryRun {
					peer.Blocks = appendID(peer.Blocks, id)
					modified[ref] = true
				}
			}
		}

		// --- Blocks → peer.BlockedBy ---
		for _, ref := range append([]string(nil), t.Blocks...) {
			if ref == id {
				issues = append(issues, Issue{Kind: Dangling, Field: FieldBlocks, TicketID: id, TargetID: ref})
				if !dryRun {
					t.Blocks = removeID(t.Blocks, ref)
					modified[id] = true
				}
				continue
			}
			peer, ok := tickets[ref]
			if !ok {
				issues = append(issues, Issue{Kind: Dangling, Field: FieldBlocks, TicketID: id, TargetID: ref})
				if !dryRun {
					t.Blocks = removeID(t.Blocks, ref)
					modified[id] = true
				}
				continue
			}
			if !containsID(peer.BlockedBy, id) {
				issues = append(issues, Issue{Kind: OneSided, Field: FieldBlocks, TicketID: id, TargetID: ref})
				if !dryRun {
					peer.BlockedBy = appendID(peer.BlockedBy, id)
					modified[ref] = true
				}
			}
		}
	}

	// Save all modified tickets.
	if !dryRun {
		for id := range modified {
			t := tickets[id]
			if err := s.Save(*t); err != nil {
				// Mark issues for this ticket as not fixed.
				continue
			}
			// Mark all issues that touched this ticket as fixed.
			for i := range issues {
				if issues[i].TicketID == id || issues[i].TargetID == id {
					issues[i].Fixed = true
				}
			}
		}
	}

	return issues, nil
}

// DoctorTicket checks links on a single ticket and optionally repairs
// them. It only examines links FROM the given ticket, not links TO it
// from other tickets.
func (s *Store) DoctorTicket(id string, dryRun bool) ([]Issue, error) {
	t, err := s.Get(id)
	if err != nil {
		return nil, err
	}

	var issues []Issue
	modified := make(map[string]*Ticket)

	getPeer := func(ref string) (*Ticket, bool) {
		if p, ok := modified[ref]; ok {
			return p, true
		}
		p, err := s.Get(ref)
		if err != nil {
			return nil, false
		}
		modified[ref] = &p
		return modified[ref], true
	}

	// Related (symmetric)
	for _, ref := range append([]string(nil), t.Related...) {
		if ref == id {
			issues = append(issues, Issue{Kind: Dangling, Field: FieldRelated, TicketID: id, TargetID: ref})
			if !dryRun {
				t.Related = removeID(t.Related, ref)
			}
			continue
		}
		peer, ok := getPeer(ref)
		if !ok {
			issues = append(issues, Issue{Kind: Dangling, Field: FieldRelated, TicketID: id, TargetID: ref})
			if !dryRun {
				t.Related = removeID(t.Related, ref)
			}
			continue
		}
		if !containsID(peer.Related, id) {
			issues = append(issues, Issue{Kind: OneSided, Field: FieldRelated, TicketID: id, TargetID: ref})
			if !dryRun {
				peer.Related = appendID(peer.Related, id)
			}
		}
	}

	// BlockedBy → peer.Blocks
	for _, ref := range append([]string(nil), t.BlockedBy...) {
		if ref == id {
			issues = append(issues, Issue{Kind: Dangling, Field: FieldBlockedBy, TicketID: id, TargetID: ref})
			if !dryRun {
				t.BlockedBy = removeID(t.BlockedBy, ref)
			}
			continue
		}
		peer, ok := getPeer(ref)
		if !ok {
			issues = append(issues, Issue{Kind: Dangling, Field: FieldBlockedBy, TicketID: id, TargetID: ref})
			if !dryRun {
				t.BlockedBy = removeID(t.BlockedBy, ref)
			}
			continue
		}
		if !containsID(peer.Blocks, id) {
			issues = append(issues, Issue{Kind: OneSided, Field: FieldBlockedBy, TicketID: id, TargetID: ref})
			if !dryRun {
				peer.Blocks = appendID(peer.Blocks, id)
			}
		}
	}

	// Blocks → peer.BlockedBy
	for _, ref := range append([]string(nil), t.Blocks...) {
		if ref == id {
			issues = append(issues, Issue{Kind: Dangling, Field: FieldBlocks, TicketID: id, TargetID: ref})
			if !dryRun {
				t.Blocks = removeID(t.Blocks, ref)
			}
			continue
		}
		peer, ok := getPeer(ref)
		if !ok {
			issues = append(issues, Issue{Kind: Dangling, Field: FieldBlocks, TicketID: id, TargetID: ref})
			if !dryRun {
				t.Blocks = removeID(t.Blocks, ref)
			}
			continue
		}
		if !containsID(peer.BlockedBy, id) {
			issues = append(issues, Issue{Kind: OneSided, Field: FieldBlocks, TicketID: id, TargetID: ref})
			if !dryRun {
				peer.BlockedBy = appendID(peer.BlockedBy, id)
			}
		}
	}

	if !dryRun && len(issues) > 0 {
		saved := true
		if err := s.Save(t); err != nil {
			saved = false
		}
		for ref, peer := range modified {
			if ref == id {
				continue
			}
			if err := s.Save(*peer); err != nil {
				saved = false
			}
		}
		if saved {
			for i := range issues {
				issues[i].Fixed = true
			}
		}
	}

	return issues, nil
}
