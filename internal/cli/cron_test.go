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
	mon := agent.NewMonitor(root, runner.Alive, runner.IdleSeconds)

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

func TestSyncAgentFrontmatterIgnoresCronOwners(t *testing.T) {
	s := newWatchStore(t)
	syncAgentFrontmatter(s.Root, agent.CronOwnerID("groomer"))
}
