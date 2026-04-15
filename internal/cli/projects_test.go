package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stepandel/tickets-md/internal/ticket"
)

func TestFilterTicketsByProject(t *testing.T) {
	tickets := []ticket.Ticket{
		{ID: "T-001", Project: "PRJ-001"},
		{ID: "T-002"},
		{ID: "T-003", Project: "PRJ-002"},
	}

	got := filterTicketsByProject(tickets, "PRJ-001")
	if len(got) != 1 || got[0].ID != "T-001" {
		t.Fatalf("filter project mismatch: %#v", got)
	}

	got = filterTicketsByProject(tickets, "-")
	if len(got) != 1 || got[0].ID != "T-002" {
		t.Fatalf("filter unassigned mismatch: %#v", got)
	}
}

func TestRunStageWizardReservedProjects(t *testing.T) {
	in := strings.NewReader("n\nprojects\nbacklog\n\n")
	var out bytes.Buffer

	got, err := runStageWizard(in, &out, []string{"backlog", "done"})
	if err != nil {
		t.Fatalf("runStageWizard: %v", err)
	}
	if len(got) != 1 || got[0] != "backlog" {
		t.Fatalf("stages = %#v, want only backlog", got)
	}
	if !strings.Contains(out.String(), `stage name "projects" is reserved`) {
		t.Fatalf("wizard output = %q, want reserved-name message", out.String())
	}
}
