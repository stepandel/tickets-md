package cli

import (
	"bytes"
	"os"
	"testing"

	"github.com/stepandel/tickets-md/internal/agent"
)

func TestDeleteTicketRemovesAgentData(t *testing.T) {
	s := newCleanupStore(t)
	tk, err := s.Create("Alpha")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := os.MkdirAll(agent.TicketDir(s.Root, tk.ID), 0o755); err != nil {
		t.Fatal(err)
	}

	var warn bytes.Buffer
	if err := deleteTicket(s, tk.ID, &warn); err != nil {
		t.Fatalf("deleteTicket: %v", err)
	}
	if warn.Len() != 0 {
		t.Fatalf("warnings = %q, want none", warn.String())
	}
	if _, err := os.Stat(agent.TicketDir(s.Root, tk.ID)); !os.IsNotExist(err) {
		t.Fatalf("agent dir still exists, stat err = %v", err)
	}
}
