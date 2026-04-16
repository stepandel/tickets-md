package cli

import (
	"testing"

	cron "github.com/robfig/cron/v3"

	"github.com/stepandel/tickets-md/internal/config"
)

func cronAgent(name, schedule string) config.CronAgentConfig {
	return config.CronAgentConfig{
		Name:     name,
		Schedule: schedule,
		Command:  "/bin/sh",
		Prompt:   "echo hi",
	}
}

func cronConfig(cronAgents ...config.CronAgentConfig) config.Config {
	return config.Config{
		Prefix:        "TIC",
		ProjectPrefix: "PRJ",
		Stages:        []string{"backlog", "execute", "done"},
		CronAgents:    cronAgents,
	}
}

func schedulerEntryID(t *testing.T, s *watchCronScheduler, name string) cron.EntryID {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[name]
	if !ok {
		t.Fatalf("entry %q missing", name)
	}
	return entry.id
}

func TestSchedulerReloadAddsNewEntries(t *testing.T) {
	s, err := startCronScheduler(t.TempDir(), cronConfig(), nil, nil)
	if err != nil {
		t.Fatalf("startCronScheduler: %v", err)
	}

	if err := s.Reload(cronConfig(cronAgent("groomer", "@every 5m"))); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if s.ActiveCount() != 1 {
		t.Fatalf("ActiveCount = %d, want 1", s.ActiveCount())
	}
	if s.engine == nil {
		t.Fatal("engine is nil, want started engine")
	}
	if got := len(s.engine.Entries()); got != 1 {
		t.Fatalf("Entries len = %d, want 1", got)
	}
	if _, ok := s.entries["groomer"]; !ok {
		t.Fatalf("entries = %#v, want groomer", s.entries)
	}
}

func TestSchedulerReloadRemovesMissingEntries(t *testing.T) {
	s, err := startCronScheduler(t.TempDir(), cronConfig(
		cronAgent("groomer", "@every 5m"),
		cronAgent("janitor", "@every 10m"),
	), nil, nil)
	if err != nil {
		t.Fatalf("startCronScheduler: %v", err)
	}

	groomerID := schedulerEntryID(t, s, "groomer")

	if err := s.Reload(cronConfig(cronAgent("groomer", "@every 5m"))); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if s.ActiveCount() != 1 {
		t.Fatalf("ActiveCount = %d, want 1", s.ActiveCount())
	}
	if got := len(s.engine.Entries()); got != 1 {
		t.Fatalf("Entries len = %d, want 1", got)
	}
	if _, ok := s.entries["janitor"]; ok {
		t.Fatalf("janitor still registered: %#v", s.entries["janitor"])
	}
	if got := schedulerEntryID(t, s, "groomer"); got != groomerID {
		t.Fatalf("groomer entry id = %d, want unchanged %d", got, groomerID)
	}
}

func TestSchedulerReloadReplacesChangedEntries(t *testing.T) {
	s, err := startCronScheduler(t.TempDir(), cronConfig(cronAgent("groomer", "@every 5m")), nil, nil)
	if err != nil {
		t.Fatalf("startCronScheduler: %v", err)
	}

	originalID := schedulerEntryID(t, s, "groomer")

	if err := s.Reload(cronConfig(cronAgent("groomer", "@every 5m"))); err != nil {
		t.Fatalf("Reload same config: %v", err)
	}
	if got := schedulerEntryID(t, s, "groomer"); got != originalID {
		t.Fatalf("entry id after identical reload = %d, want %d", got, originalID)
	}

	changedSchedule := cronAgent("groomer", "@every 10m")
	if err := s.Reload(cronConfig(changedSchedule)); err != nil {
		t.Fatalf("Reload changed schedule: %v", err)
	}
	scheduleID := schedulerEntryID(t, s, "groomer")
	if scheduleID == originalID {
		t.Fatalf("entry id after schedule change = %d, want different from %d", scheduleID, originalID)
	}

	changedCommand := changedSchedule
	changedCommand.Command = "/usr/bin/env"
	if err := s.Reload(cronConfig(changedCommand)); err != nil {
		t.Fatalf("Reload changed command: %v", err)
	}
	commandID := schedulerEntryID(t, s, "groomer")
	if commandID == scheduleID {
		t.Fatalf("entry id after command change = %d, want different from %d", commandID, scheduleID)
	}
}

func TestSchedulerReloadTreatsDisabledAsRemoved(t *testing.T) {
	s, err := startCronScheduler(t.TempDir(), cronConfig(cronAgent("groomer", "@every 5m")), nil, nil)
	if err != nil {
		t.Fatalf("startCronScheduler: %v", err)
	}

	enabled := false
	disabled := cronAgent("groomer", "@every 5m")
	disabled.Enabled = &enabled
	if err := s.Reload(cronConfig(disabled)); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	if s.ActiveCount() != 0 {
		t.Fatalf("ActiveCount = %d, want 0", s.ActiveCount())
	}
	if got := len(s.engine.Entries()); got != 0 {
		t.Fatalf("Entries len = %d, want 0", got)
	}
}

func TestSchedulerReloadSurvivesEmptyStart(t *testing.T) {
	s, err := startCronScheduler(t.TempDir(), cronConfig(), nil, nil)
	if err != nil {
		t.Fatalf("startCronScheduler: %v", err)
	}
	if s.engine != nil {
		t.Fatalf("engine = %#v, want nil before first entry", s.engine)
	}

	if err := s.Reload(cronConfig(cronAgent("groomer", "@every 5m"))); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if s.engine == nil {
		t.Fatal("engine is nil after reload")
	}
	if s.ActiveCount() != 1 {
		t.Fatalf("ActiveCount = %d, want 1", s.ActiveCount())
	}
}

func TestSchedulerReloadValidationFailurePreservesState(t *testing.T) {
	s, err := startCronScheduler(t.TempDir(), cronConfig(cronAgent("groomer", "@every 5m")), nil, nil)
	if err != nil {
		t.Fatalf("startCronScheduler: %v", err)
	}

	originalID := schedulerEntryID(t, s, "groomer")
	err = s.Reload(cronConfig(config.CronAgentConfig{
		Name:     "groomer",
		Schedule: "not-a-schedule",
		Command:  "/bin/sh",
		Prompt:   "echo hi",
	}))
	if err == nil {
		t.Fatal("Reload succeeded, want error")
	}
	if s.ActiveCount() != 1 {
		t.Fatalf("ActiveCount = %d, want 1", s.ActiveCount())
	}
	if got := schedulerEntryID(t, s, "groomer"); got != originalID {
		t.Fatalf("entry id after failed reload = %d, want unchanged %d", got, originalID)
	}
	if got := len(s.engine.Entries()); got != 1 {
		t.Fatalf("Entries len = %d, want 1", got)
	}
}
