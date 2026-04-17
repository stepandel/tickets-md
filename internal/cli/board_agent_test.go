package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/stepandel/tickets-md/internal/agent"
	"github.com/stepandel/tickets-md/internal/ticket"
)

func TestForceRerunPrompt(t *testing.T) {
	got := forceRerunPrompt("TIC-097", "TIC-097-3")
	want := "Force re-run stage agent for TIC-097? This will kill active session TIC-097-3."
	if got != want {
		t.Fatalf("forceRerunPrompt() = %q, want %q", got, want)
	}
}

func newForceRerunModel(t *testing.T, tk ticket.Ticket) *boardModel {
	t.Helper()
	s := newCleanupStore(t)
	m := &boardModel{
		store:         s,
		stages:        s.Config.Stages,
		columns:       [][]ticket.Ticket{{tk}},
		agentStatuses: map[string]agent.AgentStatus{},
		scrollOff:     []int{0},
		visibleCards:  []int{1},
	}
	return m
}

func writeRun(t *testing.T, root string, as agent.AgentStatus) {
	t.Helper()
	if as.SpawnedAt.IsZero() {
		as.SpawnedAt = time.Now().UTC()
	}
	if err := agent.Write(root, as); err != nil {
		t.Fatalf("agent.Write: %v", err)
	}
}

func TestForceRerunStageAgentConfirmOverlay(t *testing.T) {
	tk := ticket.Ticket{ID: "TIC-001"}
	m := newForceRerunModel(t, tk)

	writeRun(t, m.store.Root, agent.AgentStatus{
		TicketID: tk.ID,
		RunID:    "001-execute",
		Seq:      1,
		Stage:    "execute",
		Agent:    "claude",
		Session:  "TIC-001-1",
		Status:   agent.StatusSpawned,
	})

	m.forceRerunStageAgent()

	if _, ok := m.overlay.(*confirmOverlay); !ok {
		t.Fatalf("overlay = %T, want *confirmOverlay", m.overlay)
	}
	if m.overlayKind != "force-rerun" {
		t.Fatalf("overlayKind = %q, want %q", m.overlayKind, "force-rerun")
	}
	ctx, ok := m.overlayCtx.(forceRerunCtx)
	if !ok {
		t.Fatalf("overlayCtx = %T, want forceRerunCtx", m.overlayCtx)
	}
	if ctx.ticketID != tk.ID {
		t.Fatalf("ctx.ticketID = %q, want %q", ctx.ticketID, tk.ID)
	}
	co := m.overlay.(*confirmOverlay)
	if !strings.Contains(co.prompt, "TIC-001-1") {
		t.Fatalf("prompt = %q, expected to mention session TIC-001-1", co.prompt)
	}
}

func TestForceRerunStageAgentNoRuns(t *testing.T) {
	tk := ticket.Ticket{ID: "TIC-002"}
	m := newForceRerunModel(t, tk)

	m.forceRerunStageAgent()

	n, ok := m.overlay.(*noticeOverlay)
	if !ok {
		t.Fatalf("overlay = %T, want *noticeOverlay", m.overlay)
	}
	if n.kind != "info" || !strings.Contains(n.msg, "no active stage agent") {
		t.Fatalf("notice = {%q, %q}, want info / no active stage agent", n.kind, n.msg)
	}
	if m.overlayKind == "force-rerun" {
		t.Fatalf("overlayKind = %q, should not be force-rerun when guard trips", m.overlayKind)
	}
}

func TestForceRerunStageAgentTerminal(t *testing.T) {
	tk := ticket.Ticket{ID: "TIC-003"}
	m := newForceRerunModel(t, tk)

	writeRun(t, m.store.Root, agent.AgentStatus{
		TicketID: tk.ID,
		RunID:    "001-execute",
		Seq:      1,
		Stage:    "execute",
		Session:  "TIC-003-1",
		Status:   agent.StatusDone,
	})

	m.forceRerunStageAgent()

	if _, ok := m.overlay.(*noticeOverlay); !ok {
		t.Fatalf("overlay = %T, want *noticeOverlay", m.overlay)
	}
	if m.overlayKind == "force-rerun" {
		t.Fatalf("overlayKind = %q, terminal run must not open the confirm overlay", m.overlayKind)
	}
}

func TestForceRerunStageAgentEmptySession(t *testing.T) {
	tk := ticket.Ticket{ID: "TIC-004"}
	m := newForceRerunModel(t, tk)

	writeRun(t, m.store.Root, agent.AgentStatus{
		TicketID: tk.ID,
		RunID:    "001-execute",
		Seq:      1,
		Stage:    "execute",
		Status:   agent.StatusSpawned,
	})

	m.forceRerunStageAgent()

	if _, ok := m.overlay.(*noticeOverlay); !ok {
		t.Fatalf("overlay = %T, want *noticeOverlay", m.overlay)
	}
	if m.overlayKind == "force-rerun" {
		t.Fatalf("overlayKind = %q, empty session must not open the confirm overlay", m.overlayKind)
	}
}

func TestApplyForceRerunRejectsWrongCtx(t *testing.T) {
	tk := ticket.Ticket{ID: "TIC-005"}
	m := newForceRerunModel(t, tk)

	m.applyForceRerun("not a forceRerunCtx")
	if m.overlay != nil {
		t.Fatalf("overlay = %v, want nil (wrong ctx must no-op)", m.overlay)
	}

	m.applyForceRerun(forceRerunCtx{ticketID: ""})
	if m.overlay != nil {
		t.Fatalf("overlay = %v, want nil (empty ticket id must no-op)", m.overlay)
	}
}
