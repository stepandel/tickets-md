package cli

import (
	"testing"

	"github.com/stepandel/tickets-md/internal/ticket"
)

func TestSetFieldProject(t *testing.T) {
	tk := ticket.Ticket{}
	if err := setField(&tk, "project", "PRJ-001"); err != nil {
		t.Fatalf("setField(project): %v", err)
	}
	if tk.Project != "PRJ-001" {
		t.Fatalf("Project = %q, want PRJ-001", tk.Project)
	}
	if err := setField(&tk, "project", ""); err != nil {
		t.Fatalf("setField(clear project): %v", err)
	}
	if tk.Project != "" {
		t.Fatalf("Project = %q, want empty", tk.Project)
	}
}
