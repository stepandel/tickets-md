package agent

import (
	"context"
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

	mon := NewMonitor(root, 0, 0, 0, func(string) bool { return false }, func(string) int { return -1 }, func(string) error { return nil })
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

	mon := NewMonitor(root, 0, 0, 0, func(string) bool { return false }, func(string) int { return -1 }, func(string) error { return nil })
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

	mon := NewMonitor(root, 0, 0, 0, func(string) bool { return false }, func(string) int { return -1 }, func(string) error { return nil })
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

	mon := NewMonitor(root, 0, 0, 0, func(string) bool { return true }, func(string) int { return 0 }, func(string) error { return nil })
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

func TestPollUsesConfiguredBlockedIdleThreshold(t *testing.T) {
	root := t.TempDir()
	now := time.Now().UTC().Truncate(time.Second)
	as := AgentStatus{
		TicketID:  "TIC-112",
		RunID:     "001-execute",
		Seq:       1,
		Attempt:   1,
		Stage:     "execute",
		Agent:     "codex",
		Session:   "TIC-112-1",
		Status:    StatusRunning,
		SpawnedAt: now,
		UpdatedAt: now,
		LogFile:   LogPath(root, "TIC-112", "001-execute"),
	}
	if err := Write(root, as); err != nil {
		t.Fatalf("Write: %v", err)
	}

	mon := NewMonitor(root, 0, 3*time.Second, 0, func(string) bool { return true }, func(string) int { return 2 }, func(string) error { return nil })
	mon.poll()

	got, err := ReadRun(root, as.TicketID, as.RunID)
	if err != nil {
		t.Fatalf("ReadRun: %v", err)
	}
	if got.Status != StatusRunning {
		t.Fatalf("status = %q, want %q", got.Status, StatusRunning)
	}

	if changed := mon.SetTiming(0, time.Second, 0); !changed {
		t.Fatal("SetTiming changed = false, want true")
	}
	mon.poll()

	got, err = ReadRun(root, as.TicketID, as.RunID)
	if err != nil {
		t.Fatalf("ReadRun: %v", err)
	}
	if got.Status != StatusBlocked {
		t.Fatalf("status = %q, want %q", got.Status, StatusBlocked)
	}
}

func TestPollKillsIdleRunningSessionAfterThreshold(t *testing.T) {
	root := t.TempDir()
	now := time.Now().UTC().Truncate(time.Second)
	as := AgentStatus{
		TicketID:  "TIC-126",
		RunID:     "001-execute",
		Seq:       1,
		Attempt:   1,
		Stage:     "execute",
		Agent:     "codex",
		Session:   "TIC-126-1",
		Status:    StatusRunning,
		SpawnedAt: now,
		UpdatedAt: now,
		LogFile:   LogPath(root, "TIC-126", "001-execute"),
	}
	if err := Write(root, as); err != nil {
		t.Fatalf("Write: %v", err)
	}

	killed := 0
	mon := NewMonitor(root, 0, time.Hour, 3*time.Second, func(string) bool { return true }, func(string) int { return 5 }, func(session string) error {
		killed++
		if session != "TIC-126-1" {
			t.Fatalf("kill session = %q, want %q", session, "TIC-126-1")
		}
		return nil
	})

	mon.poll()

	if killed != 1 {
		t.Fatalf("kill count = %d, want 1", killed)
	}
	got, err := ReadRun(root, as.TicketID, as.RunID)
	if err != nil {
		t.Fatalf("ReadRun: %v", err)
	}
	if got.Status != StatusFailed {
		t.Fatalf("status = %q, want %q", got.Status, StatusFailed)
	}
	if got.Error != "session killed after 5s idle" {
		t.Fatalf("error = %q, want %q", got.Error, "session killed after 5s idle")
	}
}

func TestPollKillsIdleBlockedSessionAfterThreshold(t *testing.T) {
	root := t.TempDir()
	now := time.Now().UTC().Truncate(time.Second)
	as := AgentStatus{
		TicketID:  "TIC-127",
		RunID:     "001-execute",
		Seq:       1,
		Attempt:   1,
		Stage:     "execute",
		Agent:     "codex",
		Session:   "TIC-127-1",
		Status:    StatusBlocked,
		SpawnedAt: now,
		UpdatedAt: now,
		LogFile:   LogPath(root, "TIC-127", "001-execute"),
	}
	if err := Write(root, as); err != nil {
		t.Fatalf("Write: %v", err)
	}

	killed := 0
	mon := NewMonitor(root, 0, 2*time.Second, 3*time.Second, func(string) bool { return true }, func(string) int { return 5 }, func(string) error {
		killed++
		return nil
	})

	mon.poll()

	if killed != 1 {
		t.Fatalf("kill count = %d, want 1", killed)
	}
	got, err := ReadRun(root, as.TicketID, as.RunID)
	if err != nil {
		t.Fatalf("ReadRun: %v", err)
	}
	if got.Status != StatusFailed {
		t.Fatalf("status = %q, want %q", got.Status, StatusFailed)
	}
	if got.Error != "session killed after 5s idle" {
		t.Fatalf("error = %q, want %q", got.Error, "session killed after 5s idle")
	}
}

func TestPollDoesNotKillWhenIdleKillDisabled(t *testing.T) {
	root := t.TempDir()
	now := time.Now().UTC().Truncate(time.Second)
	as := AgentStatus{
		TicketID:  "TIC-128",
		RunID:     "001-execute",
		Seq:       1,
		Attempt:   1,
		Stage:     "execute",
		Agent:     "codex",
		Session:   "TIC-128-1",
		Status:    StatusRunning,
		SpawnedAt: now,
		UpdatedAt: now,
		LogFile:   LogPath(root, "TIC-128", "001-execute"),
	}
	if err := Write(root, as); err != nil {
		t.Fatalf("Write: %v", err)
	}

	mon := NewMonitor(root, 0, time.Hour, 0, func(string) bool { return true }, func(string) int { return 100 }, func(string) error {
		t.Fatal("kill should not be called when idle kill is disabled")
		return nil
	})

	mon.poll()

	got, err := ReadRun(root, as.TicketID, as.RunID)
	if err != nil {
		t.Fatalf("ReadRun: %v", err)
	}
	if got.Status != StatusRunning {
		t.Fatalf("status = %q, want %q", got.Status, StatusRunning)
	}
	if got.Error != "" {
		t.Fatalf("error = %q, want empty", got.Error)
	}
}

func TestMonitorRunResetsTickerOnTimingReload(t *testing.T) {
	root := t.TempDir()
	now := time.Now().UTC().Truncate(time.Second)
	as := AgentStatus{
		TicketID:  "TIC-115",
		RunID:     "001-execute",
		Seq:       1,
		Attempt:   1,
		Stage:     "execute",
		Agent:     "codex",
		Session:   "TIC-115-1",
		Status:    StatusRunning,
		SpawnedAt: now,
		UpdatedAt: now,
		LogFile:   LogPath(root, "TIC-115", "001-execute"),
	}
	if err := Write(root, as); err != nil {
		t.Fatalf("Write: %v", err)
	}

	polls := make(chan time.Time, 4)
	mon := NewMonitor(root, time.Hour, DefaultBlockedIdle, 0, func(string) bool {
		select {
		case polls <- time.Now():
		default:
		}
		return true
	}, func(string) int { return 0 }, func(string) error { return nil })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go mon.Run(ctx)

	if changed := mon.SetTiming(20*time.Millisecond, DefaultBlockedIdle, 0); !changed {
		t.Fatal("SetTiming changed = false, want true")
	}

	select {
	case <-polls:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("monitor did not poll after timing reload")
	}
}
