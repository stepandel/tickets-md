package cli

import (
	"os"
	"testing"
	"time"

	"github.com/stepandel/tickets-md/internal/agent"
	"github.com/stepandel/tickets-md/internal/config"
)

func TestSpawnCronAgentImmediateExitMarksRunDone(t *testing.T) {
	root := t.TempDir()
	if _, err := agent.ListAll(root); err != nil && !os.IsNotExist(err) {
		t.Fatalf("ListAll: %v", err)
	}
	if err := os.MkdirAll(agent.CronRunsDir(root, "groomer"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	runner := agent.NewPTYRunner()
	mon := agent.NewMonitor(root, 0, 0, 0, runner.Alive, runner.IdleSeconds, runner.Kill)

	err := spawnCronAgent(root, config.CronAgentConfig{
		Name:     "groomer",
		Schedule: "@every 5m",
		Command:  "/bin/sh",
		Args:     []string{"-c", "exit 0"},
		Prompt:   "ignored",
	}, mon, runner)
	if err != nil {
		t.Fatalf("spawnCronAgent: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		as, err := agent.CronLatest(root, "groomer")
		if err == nil && as.Status == agent.StatusDone {
			if as.ExitCode != nil && *as.ExitCode != 0 {
				t.Fatalf("exit code = %v, want nil or 0", as.ExitCode)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	as, err := agent.CronLatest(root, "groomer")
	if err != nil {
		t.Fatalf("CronLatest: %v", err)
	}
	t.Fatalf("latest status = %q, want %q", as.Status, agent.StatusDone)
}

func TestSpawnCronAgentNonClaudeArgsReachDone(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(agent.CronRunsDir(root, "codex-groomer"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	runner := agent.NewPTYRunner()
	mon := agent.NewMonitor(root, 0, 0, 0, runner.Alive, runner.IdleSeconds, runner.Kill)

	err := spawnCronAgent(root, config.CronAgentConfig{
		Name:     "codex-groomer",
		Schedule: "@every 5m",
		Command:  "/bin/sh",
		Args:     []string{"-c", "printf '%s\n' \"$1\"; exit 0", "cron-wrapper"},
		Prompt:   "review the backlog",
	}, mon, runner)
	if err != nil {
		t.Fatalf("spawnCronAgent: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		as, err := agent.CronLatest(root, "codex-groomer")
		if err == nil && as.Status == agent.StatusDone {
			if as.ExitCode != nil && *as.ExitCode != 0 {
				t.Fatalf("exit code = %v, want nil or 0", as.ExitCode)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	as, err := agent.CronLatest(root, "codex-groomer")
	if err != nil {
		t.Fatalf("CronLatest: %v", err)
	}
	t.Fatalf("latest status = %q, want %q", as.Status, agent.StatusDone)
}

func TestSyncAgentFrontmatterIgnoresCronOwners(t *testing.T) {
	s := newWatchStore(t)
	syncAgentFrontmatter(s.Root, agent.CronOwnerID("groomer"))
}

type fakeCronIntegration struct {
	name       string
	stageCalls int
	cronCalls  int
}

func (f *fakeCronIntegration) Name() string { return f.name }

func (f *fakeCronIntegration) PrepareArgs(argv []string) ([]string, string, error) {
	f.stageCalls++
	return argv, "stage-session", nil
}

func (f *fakeCronIntegration) PrepareCronArgs(argv []string) ([]string, string, error) {
	f.cronCalls++
	return append([]string{"--cron-prepared"}, argv...), "cron-session", nil
}

func (f *fakeCronIntegration) ExtractPlan(sessionID, cwd string) (string, error) {
	return "", nil
}

func TestStartCronRunPrefersCronIntegrationHook(t *testing.T) {
	root := t.TempDir()
	fake := &fakeCronIntegration{name: "fake-cron-test-agent"}
	agent.Register(fake)

	if err := os.MkdirAll(agent.CronRunsDir(root, "fake-cron"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	runner := agent.NewPTYRunner()
	mon := agent.NewMonitor(root, 0, 0, 0, runner.Alive, runner.IdleSeconds, runner.Kill)

	// spawnCronAgent will error when exec fails for the fake command name,
	// but the integration hook runs and persists SessionUUID before spawn.
	_ = spawnCronAgent(root, config.CronAgentConfig{
		Name:     "fake-cron",
		Schedule: "@every 5m",
		Command:  fake.name,
		Args:     []string{"--flag"},
		Prompt:   "ignored",
	}, mon, runner)

	if fake.cronCalls != 1 {
		t.Fatalf("cronCalls = %d, want 1", fake.cronCalls)
	}
	if fake.stageCalls != 0 {
		t.Fatalf("stageCalls = %d, want 0", fake.stageCalls)
	}

	as, err := agent.CronLatest(root, "fake-cron")
	if err != nil {
		t.Fatalf("CronLatest: %v", err)
	}
	if as.SessionUUID != "cron-session" {
		t.Fatalf("SessionUUID = %q, want %q", as.SessionUUID, "cron-session")
	}
}

func TestStartCronRunInteractivePrefersPrepareArgs(t *testing.T) {
	root := t.TempDir()
	fake := &fakeCronIntegration{name: "fake-cron-interactive-test-agent"}
	agent.Register(fake)

	if err := os.MkdirAll(agent.CronRunsDir(root, "fake-cron-interactive"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	runner := agent.NewPTYRunner()
	mon := agent.NewMonitor(root, 0, 0, 0, runner.Alive, runner.IdleSeconds, runner.Kill)

	_ = spawnCronAgent(root, config.CronAgentConfig{
		Name:        "fake-cron-interactive",
		Schedule:    "@every 5m",
		Command:     fake.name,
		Args:        []string{"--flag"},
		Prompt:      "ignored",
		Interactive: true,
	}, mon, runner)

	if fake.stageCalls != 1 {
		t.Fatalf("stageCalls = %d, want 1", fake.stageCalls)
	}
	if fake.cronCalls != 0 {
		t.Fatalf("cronCalls = %d, want 0", fake.cronCalls)
	}

	as, err := agent.CronLatest(root, "fake-cron-interactive")
	if err != nil {
		t.Fatalf("CronLatest: %v", err)
	}
	if as.SessionUUID != "stage-session" {
		t.Fatalf("SessionUUID = %q, want %q", as.SessionUUID, "stage-session")
	}
}

func TestStartCronRunNonInteractiveStillUsesPrepareCronArgs(t *testing.T) {
	root := t.TempDir()
	fake := &fakeCronIntegration{name: "fake-cron-noninteractive-test-agent"}
	agent.Register(fake)

	if err := os.MkdirAll(agent.CronRunsDir(root, "fake-cron-noninteractive"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	runner := agent.NewPTYRunner()
	mon := agent.NewMonitor(root, 0, 0, 0, runner.Alive, runner.IdleSeconds, runner.Kill)

	_ = spawnCronAgent(root, config.CronAgentConfig{
		Name:     "fake-cron-noninteractive",
		Schedule: "@every 5m",
		Command:  fake.name,
		Args:     []string{"--flag"},
		Prompt:   "ignored",
	}, mon, runner)

	if fake.cronCalls != 1 {
		t.Fatalf("cronCalls = %d, want 1", fake.cronCalls)
	}
	if fake.stageCalls != 0 {
		t.Fatalf("stageCalls = %d, want 0", fake.stageCalls)
	}
}
