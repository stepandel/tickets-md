package cli

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/stepandel/tickets-md/internal/agent"
	"github.com/stepandel/tickets-md/internal/config"
	"github.com/stepandel/tickets-md/internal/ticket"
)

func newHarnessStore(t *testing.T) *ticket.Store {
	t.Helper()
	root := t.TempDir()
	s, err := ticket.Init(root, config.Config{
		Prefix: "T",
		Stages: []string{"backlog", "execute", "done"},
	})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	return s
}

// writeRunRaw plants an AgentStatus on disk at the exact timestamps
// given, bypassing agent.Write's transition validation and UpdatedAt
// stamping. Tests use this to simulate states the real code path
// wouldn't produce directly (e.g. a "running" run with a 48h-old
// UpdatedAt for the stale-run check).
func writeRunRaw(t *testing.T, root string, as agent.AgentStatus) {
	t.Helper()
	dir := agent.TicketDir(root, as.TicketID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := yaml.Marshal(as)
	if err != nil {
		t.Fatalf("yaml marshal: %v", err)
	}
	path := filepath.Join(dir, as.RunID+".yml")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write run file: %v", err)
	}
}

func TestHarnessDoctorNothingToDo(t *testing.T) {
	s := newHarnessStore(t)
	_, _ = s.Create("Alpha")

	issues, err := HarnessDoctor(s, false, 0)
	if err != nil {
		t.Fatalf("HarnessDoctor: %v", err)
	}
	if len(issues) != 0 {
		t.Errorf("expected 0 issues on a clean store, got %d: %v", len(issues), issues)
	}
}

func TestHarnessDoctorStaleRun(t *testing.T) {
	s := newHarnessStore(t)
	tk, _ := s.Create("Alpha")

	runID := agent.FormatRunID(1, "execute")
	writeRunRaw(t, s.Root, agent.AgentStatus{
		TicketID:  tk.ID,
		RunID:     runID,
		Seq:       1,
		Attempt:   1,
		Stage:     "execute",
		Agent:     "claude",
		Session:   tk.ID + "-1",
		Status:    agent.StatusRunning,
		SpawnedAt: time.Now().Add(-48 * time.Hour).UTC().Truncate(time.Second),
		UpdatedAt: time.Now().Add(-48 * time.Hour).UTC().Truncate(time.Second),
		LogFile:   agent.LogPath(s.Root, tk.ID, runID),
	})

	issues, err := HarnessDoctor(s, true, 24*time.Hour)
	if err != nil {
		t.Fatalf("HarnessDoctor: %v", err)
	}

	var found *HarnessIssue
	for i := range issues {
		if issues[i].Kind == StaleRun {
			found = &issues[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected a StaleRun issue, got %v", issues)
	}
	if !found.Fixed {
		t.Errorf("stale run issue should be fixed: %v", found)
	}

	latest, err := agent.Latest(s.Root, tk.ID)
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if latest.Status != agent.StatusFailed {
		t.Errorf("run status = %q, want failed", latest.Status)
	}
}

func TestHarnessDoctorStaleRunDryRun(t *testing.T) {
	s := newHarnessStore(t)
	tk, _ := s.Create("Alpha")

	runID := agent.FormatRunID(1, "execute")
	writeRunRaw(t, s.Root, agent.AgentStatus{
		TicketID:  tk.ID,
		RunID:     runID,
		Seq:       1,
		Stage:     "execute",
		Agent:     "claude",
		Session:   tk.ID + "-1",
		Status:    agent.StatusRunning,
		SpawnedAt: time.Now().Add(-48 * time.Hour).UTC().Truncate(time.Second),
		UpdatedAt: time.Now().Add(-48 * time.Hour).UTC().Truncate(time.Second),
	})

	issues, err := HarnessDoctor(s, false, 24*time.Hour)
	if err != nil {
		t.Fatalf("HarnessDoctor: %v", err)
	}
	for _, iss := range issues {
		if iss.Fixed {
			t.Errorf("dry-run should not fix anything, got %v", iss)
		}
	}
	latest, _ := agent.Latest(s.Root, tk.ID)
	if latest.Status != agent.StatusRunning {
		t.Errorf("dry-run changed status on disk: %q", latest.Status)
	}
}

func TestHarnessDoctorFreshRunIsNotStale(t *testing.T) {
	s := newHarnessStore(t)
	tk, _ := s.Create("Alpha")

	runID := agent.FormatRunID(1, "execute")
	writeRunRaw(t, s.Root, agent.AgentStatus{
		TicketID:  tk.ID,
		RunID:     runID,
		Seq:       1,
		Stage:     "execute",
		Agent:     "claude",
		Session:   tk.ID + "-1",
		Status:    agent.StatusRunning,
		SpawnedAt: time.Now().UTC().Truncate(time.Second),
		UpdatedAt: time.Now().UTC().Truncate(time.Second),
	})

	issues, err := HarnessDoctor(s, true, 24*time.Hour)
	if err != nil {
		t.Fatalf("HarnessDoctor: %v", err)
	}
	for _, iss := range issues {
		if iss.Kind == StaleRun {
			t.Errorf("fresh run should not be flagged stale: %v", iss)
		}
	}
}

func TestHarnessDoctorOrphanAgentDir(t *testing.T) {
	s := newHarnessStore(t)

	// Plant an .agents/ subdir for a ticket that doesn't exist.
	ghost := "T-999"
	ghostDir := agent.TicketDir(s.Root, ghost)
	if err := os.MkdirAll(ghostDir, 0o755); err != nil {
		t.Fatal(err)
	}

	issues, err := HarnessDoctor(s, true, 0)
	if err != nil {
		t.Fatalf("HarnessDoctor: %v", err)
	}
	var found *HarnessIssue
	for i := range issues {
		if issues[i].Kind == OrphanAgentDir && issues[i].Target == ghost {
			found = &issues[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected OrphanAgentDir for %s, got %v", ghost, issues)
	}
	if !found.Fixed {
		t.Errorf("orphan agent dir should be fixed: %v", found)
	}
	if _, err := os.Stat(ghostDir); !os.IsNotExist(err) {
		t.Errorf("expected %s removed, stat err = %v", ghostDir, err)
	}
}

func TestHarnessDoctorOrphanAgentDirDryRun(t *testing.T) {
	s := newHarnessStore(t)
	ghostDir := agent.TicketDir(s.Root, "T-999")
	if err := os.MkdirAll(ghostDir, 0o755); err != nil {
		t.Fatal(err)
	}

	issues, err := HarnessDoctor(s, false, 0)
	if err != nil {
		t.Fatalf("HarnessDoctor: %v", err)
	}
	for _, iss := range issues {
		if iss.Fixed {
			t.Errorf("dry-run should not remove anything, got %v", iss)
		}
	}
	if _, err := os.Stat(ghostDir); err != nil {
		t.Errorf("dry-run removed orphan dir: %v", err)
	}
}

func TestHarnessDoctorOrphanTmpFile(t *testing.T) {
	s := newHarnessStore(t)
	tk, _ := s.Create("Alpha")

	runID := agent.FormatRunID(1, "execute")
	writeRunRaw(t, s.Root, agent.AgentStatus{
		TicketID:  tk.ID,
		RunID:     runID,
		Seq:       1,
		Stage:     "execute",
		Agent:     "claude",
		Session:   tk.ID + "-1",
		Status:    agent.StatusDone,
		SpawnedAt: time.Now().UTC().Truncate(time.Second),
		UpdatedAt: time.Now().UTC().Truncate(time.Second),
	})
	tmp := filepath.Join(agent.TicketDir(s.Root, tk.ID), "002-execute.yml.tmp")
	if err := os.WriteFile(tmp, []byte("partial"), 0o644); err != nil {
		t.Fatal(err)
	}

	issues, err := HarnessDoctor(s, true, 0)
	if err != nil {
		t.Fatalf("HarnessDoctor: %v", err)
	}
	var found *HarnessIssue
	for i := range issues {
		if issues[i].Kind == OrphanTmp {
			found = &issues[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected OrphanTmp, got %v", issues)
	}
	if !found.Fixed {
		t.Errorf("orphan tmp should be fixed: %v", found)
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Errorf("expected %s removed, stat err = %v", tmp, err)
	}
}

func TestAutoHealFixesSafeIssuesOnly(t *testing.T) {
	s := newHarnessStore(t)
	tk, _ := s.Create("Alpha")

	// Safe fix #1: frontmatter drift (done run vs "running" frontmatter).
	runID := agent.FormatRunID(1, "execute")
	writeRunRaw(t, s.Root, agent.AgentStatus{
		TicketID:  tk.ID,
		RunID:     runID,
		Seq:       1,
		Stage:     "execute",
		Agent:     "claude",
		Session:   tk.ID + "-1",
		Status:    agent.StatusDone,
		SpawnedAt: time.Now().UTC().Truncate(time.Second),
		UpdatedAt: time.Now().UTC().Truncate(time.Second),
	})
	tk, _ = s.Get(tk.ID)
	tk.AgentStatus = "running"
	if err := s.Save(tk); err != nil {
		t.Fatal(err)
	}

	// Safe fix #2: an orphan .yml.tmp file.
	tmp := filepath.Join(agent.TicketDir(s.Root, tk.ID), "002-execute.yml.tmp")
	if err := os.WriteFile(tmp, []byte("partial"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Destructive issue that auto must *not* touch: a stale run.
	ghost, _ := s.Create("Ghost")
	staleRun := agent.FormatRunID(1, "execute")
	writeRunRaw(t, s.Root, agent.AgentStatus{
		TicketID:  ghost.ID,
		RunID:     staleRun,
		Seq:       1,
		Stage:     "execute",
		Agent:     "claude",
		Session:   ghost.ID + "-1",
		Status:    agent.StatusRunning,
		SpawnedAt: time.Now().Add(-48 * time.Hour).UTC().Truncate(time.Second),
		UpdatedAt: time.Now().Add(-48 * time.Hour).UTC().Truncate(time.Second),
	})

	issues, err := AutoHeal(s)
	if err != nil {
		t.Fatalf("AutoHeal: %v", err)
	}

	// Frontmatter drift fixed.
	tk, _ = s.Get(tk.ID)
	if tk.AgentStatus != "done" {
		t.Errorf("auto-heal did not fix frontmatter: status=%q", tk.AgentStatus)
	}
	// Tmp file removed.
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Errorf("auto-heal did not remove %s: stat err=%v", tmp, err)
	}
	// Stale run left alone.
	stale, err := agent.Latest(s.Root, ghost.ID)
	if err != nil {
		t.Fatalf("Latest(ghost): %v", err)
	}
	if stale.Status != agent.StatusRunning {
		t.Errorf("auto-heal flipped stale run: status=%q, want running", stale.Status)
	}
	// Every reported issue must be Fixed (auto always fixes what it reports).
	for _, iss := range issues {
		if !iss.Fixed {
			t.Errorf("auto-heal reported unfixed issue: %v", iss)
		}
	}
}

func TestHarnessDoctorFrontmatterDrift(t *testing.T) {
	s := newHarnessStore(t)
	tk, _ := s.Create("Alpha")

	// Run finished — status should be "done" in frontmatter.
	runID := agent.FormatRunID(1, "execute")
	writeRunRaw(t, s.Root, agent.AgentStatus{
		TicketID:  tk.ID,
		RunID:     runID,
		Seq:       1,
		Stage:     "execute",
		Agent:     "claude",
		Session:   tk.ID + "-1",
		Status:    agent.StatusDone,
		SpawnedAt: time.Now().UTC().Truncate(time.Second),
		UpdatedAt: time.Now().UTC().Truncate(time.Second),
	})

	tk, _ = s.Get(tk.ID)
	tk.AgentStatus = "running"
	tk.AgentRun = "999-execute"
	tk.AgentSession = "stale"
	if err := s.Save(tk); err != nil {
		t.Fatal(err)
	}

	issues, err := HarnessDoctor(s, true, 0)
	if err != nil {
		t.Fatalf("HarnessDoctor: %v", err)
	}
	var found *HarnessIssue
	for i := range issues {
		if issues[i].Kind == FrontmatterDrift && issues[i].Target == tk.ID {
			found = &issues[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected FrontmatterDrift, got %v", issues)
	}
	if !found.Fixed {
		t.Errorf("drift should be fixed: %v", found)
	}

	tk, _ = s.Get(tk.ID)
	if tk.AgentStatus != "done" {
		t.Errorf("agent_status = %q, want done", tk.AgentStatus)
	}
	if tk.AgentRun != runID {
		t.Errorf("agent_run = %q, want %q", tk.AgentRun, runID)
	}
	// Terminal runs should not leak a session value into the frontmatter.
	if tk.AgentSession != "" {
		t.Errorf("agent_session = %q, want empty for terminal run", tk.AgentSession)
	}
}
