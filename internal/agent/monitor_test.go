package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestPollDoesNotFailTrackedSpawnedRunWhenSessionMissing(t *testing.T) {
	root := t.TempDir()
	now := time.Now().UTC().Truncate(time.Second)
	as := AgentStatus{
		TicketID:  CronOwnerID("backlog-groomer"),
		RunID:     "001-cron",
		Seq:       1,
		Attempt:   1,
		Stage:     "cron",
		Agent:     "codex",
		Session:   ".cron-backlog-groomer-1",
		Status:    StatusSpawned,
		SpawnedAt: now,
		LogFile:   CronLogPath(root, "backlog-groomer", "001-cron"),
	}
	if err := Write(root, as); err != nil {
		t.Fatalf("Write: %v", err)
	}

	mon := NewMonitor(root, func(string) bool { return false }, func(string) int { return -1 })
	mon.TrackRun(as.TicketID, as.RunID)
	mon.poll()

	got, err := ReadRun(root, as.TicketID, as.RunID)
	if err != nil {
		t.Fatalf("ReadRun: %v", err)
	}
	if got.Status != StatusSpawned {
		t.Fatalf("status = %q, want %q", got.Status, StatusSpawned)
	}
	if got.Error != "" {
		t.Fatalf("error = %q, want empty", got.Error)
	}
}

// writeRunRaw plants an AgentStatus on disk at the exact timestamps
// given, bypassing agent.Write's transition validation and UpdatedAt
// stamping. Tests use this to simulate states the real code path
// wouldn't produce directly (e.g. a terminal cron run with a 48h-old
// UpdatedAt for the stale-file GC check).
func writeRunRaw(t *testing.T, root string, as AgentStatus) {
	t.Helper()
	dir := TicketDir(root, as.TicketID)
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

func TestPollKeepsCronDirAfterRunsExpire(t *testing.T) {
	root := t.TempDir()
	name := "backlog-groomer"
	runID := "001-cron"
	now := time.Now().UTC().Truncate(time.Second)
	writeRunRaw(t, root, AgentStatus{
		TicketID:  CronOwnerID(name),
		RunID:     runID,
		Seq:       1,
		Attempt:   1,
		Stage:     "cron",
		Agent:     "codex",
		Session:   ".cron-backlog-groomer-1",
		Status:    StatusDone,
		SpawnedAt: now.Add(-48 * time.Hour),
		UpdatedAt: now.Add(-48 * time.Hour),
		LogFile:   CronLogPath(root, name, runID),
	})
	if err := os.MkdirAll(filepath.Dir(CronLogPath(root, name, runID)), 0o755); err != nil {
		t.Fatalf("MkdirAll runs dir: %v", err)
	}
	if err := os.WriteFile(CronLogPath(root, name, runID), []byte("done"), 0o644); err != nil {
		t.Fatalf("WriteFile log: %v", err)
	}

	mon := NewMonitor(root, func(string) bool { return false }, func(string) int { return -1 })
	mon.poll()

	if _, err := os.Stat(CronDir(root, name)); err != nil {
		t.Fatalf("cron dir removed: %v", err)
	}
	// The stale run YAML and log are pruned by the per-run-file GC
	// near the top of poll() (not by the dir-prune step). The owner
	// dir must survive either way.
	if _, err := os.Stat(runPath(root, CronOwnerID(name), runID)); !os.IsNotExist(err) {
		t.Fatalf("expected stale run pruned, got %v", err)
	}
	if _, err := os.Stat(CronLogPath(root, name, runID)); !os.IsNotExist(err) {
		t.Fatalf("expected stale log pruned, got %v", err)
	}
}

func TestPollKeepsOrphanCronDirForDoctor(t *testing.T) {
	root := t.TempDir()
	name := "ghost-groomer"
	runID := "001-cron"
	now := time.Now().UTC().Truncate(time.Second)
	writeRunRaw(t, root, AgentStatus{
		TicketID:  CronOwnerID(name),
		RunID:     runID,
		Seq:       1,
		Attempt:   1,
		Stage:     "cron",
		Agent:     "codex",
		Session:   ".cron-ghost-groomer-1",
		Status:    StatusFailed,
		SpawnedAt: now.Add(-48 * time.Hour),
		UpdatedAt: now.Add(-48 * time.Hour),
		LogFile:   CronLogPath(root, name, runID),
	})

	mon := NewMonitor(root, func(string) bool { return false }, func(string) int { return -1 })
	mon.poll()

	if _, err := os.Stat(CronDir(root, name)); err != nil {
		t.Fatalf("cron dir removed: %v", err)
	}
}

func TestPollKeepsActiveCronDir(t *testing.T) {
	root := t.TempDir()
	name := "groomer"
	runID := "001-cron"
	now := time.Now().UTC().Truncate(time.Second)
	writeRunRaw(t, root, AgentStatus{
		TicketID:  CronOwnerID(name),
		RunID:     runID,
		Seq:       1,
		Attempt:   1,
		Stage:     "cron",
		Agent:     "codex",
		Session:   ".cron-groomer-1",
		Status:    StatusRunning,
		SpawnedAt: now,
		UpdatedAt: now,
		LogFile:   CronLogPath(root, name, runID),
	})

	mon := NewMonitor(root, func(string) bool { return true }, func(string) int { return 0 })
	mon.poll()

	if _, err := os.Stat(CronDir(root, name)); err != nil {
		t.Fatalf("cron dir removed: %v", err)
	}
	got, err := CronReadRun(root, name, runID)
	if err != nil {
		t.Fatalf("CronReadRun: %v", err)
	}
	if got.Status != StatusRunning {
		t.Fatalf("status = %q, want %q", got.Status, StatusRunning)
	}
}
