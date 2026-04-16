package agent

import (
	"testing"
	"time"
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
