package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTransitionAllowedEdges(t *testing.T) {
	allowed := []struct{ from, to Status }{
		{StatusSpawned, StatusRunning},
		{StatusSpawned, StatusDone},
		{StatusSpawned, StatusErrored},
		{StatusSpawned, StatusFailed},
		{StatusRunning, StatusDone},
		{StatusRunning, StatusFailed},
		{StatusRunning, StatusBlocked},
		{StatusBlocked, StatusRunning},
		{StatusBlocked, StatusDone},
		{StatusBlocked, StatusFailed},
	}
	for _, tc := range allowed {
		if err := Transition(tc.from, tc.to); err != nil {
			t.Errorf("Transition(%q → %q) unexpectedly rejected: %v", tc.from, tc.to, err)
		}
	}
}

func TestTransitionRejectsFromTerminal(t *testing.T) {
	for _, from := range []Status{StatusDone, StatusFailed, StatusErrored} {
		for _, to := range []Status{StatusSpawned, StatusRunning, StatusBlocked, StatusDone, StatusFailed, StatusErrored} {
			if err := Transition(from, to); err == nil {
				t.Errorf("Transition(%q → %q) should be rejected — %q is terminal", from, to, from)
			}
		}
	}
}

func TestTransitionRejectsInvalidEdges(t *testing.T) {
	invalid := []struct{ from, to Status }{
		{StatusSpawned, StatusBlocked}, // can't skip to blocked
		{StatusRunning, StatusSpawned}, // can't regress
		{StatusRunning, StatusErrored}, // errored is only for spawn failures
		{StatusBlocked, StatusSpawned}, // can't regress
	}
	for _, tc := range invalid {
		if err := Transition(tc.from, tc.to); err == nil {
			t.Errorf("Transition(%q → %q) should be rejected", tc.from, tc.to)
		}
	}
}

func TestIsTerminal(t *testing.T) {
	terminal := []Status{StatusDone, StatusFailed, StatusErrored}
	for _, s := range terminal {
		if !s.IsTerminal() {
			t.Errorf("%q should be terminal", s)
		}
	}
	nonTerminal := []Status{StatusSpawned, StatusRunning, StatusBlocked}
	for _, s := range nonTerminal {
		if s.IsTerminal() {
			t.Errorf("%q should not be terminal", s)
		}
	}
}

func TestCronHelpersRoundTrip(t *testing.T) {
	root := t.TempDir()
	runID, seq, attempt, err := CronNextRun(root, "backlog-groomer")
	if err != nil {
		t.Fatalf("CronNextRun: %v", err)
	}
	if runID != "001-cron" || seq != 1 || attempt != 1 {
		t.Fatalf("CronNextRun = (%q, %d, %d), want (001-cron, 1, 1)", runID, seq, attempt)
	}

	now := time.Now().UTC().Truncate(time.Second)
	as := AgentStatus{
		TicketID:  CronOwnerID("backlog-groomer"),
		RunID:     runID,
		Seq:       seq,
		Attempt:   attempt,
		Stage:     "cron",
		Agent:     "claude",
		Session:   ".cron-backlog-groomer-1",
		Status:    StatusSpawned,
		SpawnedAt: now,
		LogFile:   CronLogPath(root, "backlog-groomer", runID),
	}
	if err := os.MkdirAll(CronRunsDir(root, "backlog-groomer"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := Write(root, as); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := os.Stat(filepath.Join(CronDir(root, "backlog-groomer"), runID+".yml")); err != nil {
		t.Fatalf("Stat run file: %v", err)
	}

	latest, err := CronLatest(root, "backlog-groomer")
	if err != nil {
		t.Fatalf("CronLatest: %v", err)
	}
	if latest.TicketID != CronOwnerID("backlog-groomer") || latest.RunID != runID {
		t.Fatalf("CronLatest = %#v", latest)
	}

	if statuses, err := List(root); err != nil {
		t.Fatalf("List: %v", err)
	} else if len(statuses) != 0 {
		t.Fatalf("List() returned cron rows: %#v", statuses)
	}

	allCron, err := ListAllCronRuns(root)
	if err != nil {
		t.Fatalf("ListAllCronRuns: %v", err)
	}
	if len(allCron) != 1 || allCron[0].RunID != runID {
		t.Fatalf("ListAllCronRuns = %#v", allCron)
	}
}
