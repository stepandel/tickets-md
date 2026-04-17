package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stepandel/tickets-md/internal/agent"
	"github.com/stepandel/tickets-md/internal/config"
	"github.com/stepandel/tickets-md/internal/ticket"
	"github.com/stepandel/tickets-md/internal/worktree"
)

func runCleanupGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func newCleanupStore(t *testing.T) *ticket.Store {
	return newCleanupStoreWithConfig(t, config.Config{
		Prefix:        "TIC",
		ProjectPrefix: "PRJ",
		Stages:        []string{"backlog", "execute", "done"},
	})
}

func newCleanupStoreWithConfig(t *testing.T, cfg config.Config) *ticket.Store {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	runCleanupGit(t, root, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("init\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	runCleanupGit(t, root, "add", "README.md")
	runCleanupGit(t, root, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-m", "init")

	s, err := ticket.Init(root, cfg)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	return s
}

func TestFindOrphanAgentIDs(t *testing.T) {
	s := newCleanupStore(t)
	if err := os.MkdirAll(agent.TicketDir(s.Root, "TIC-999"), 0o755); err != nil {
		t.Fatal(err)
	}

	ids, err := findOrphanAgentIDs(s)
	if err != nil {
		t.Fatalf("findOrphanAgentIDs: %v", err)
	}
	if len(ids) != 1 || ids[0] != "TIC-999" {
		t.Fatalf("ids = %v, want [TIC-999]", ids)
	}
}

func TestFindOrphanAgentIDsSkipsCronNamespace(t *testing.T) {
	s := newCleanupStore(t)
	if err := os.MkdirAll(agent.CronDir(s.Root, "groomer"), 0o755); err != nil {
		t.Fatal(err)
	}

	ids, err := findOrphanAgentIDs(s)
	if err != nil {
		t.Fatalf("findOrphanAgentIDs: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("ids = %v, want empty — cron dir must not be flagged as an orphan ticket", ids)
	}
}

func TestFindOrphanWorktreeAndBranchIDs(t *testing.T) {
	s := newCleanupStore(t)
	layout := worktreeLayout(s.Config)
	if _, err := worktree.Create(s.Root, layout, "TIC-999", ""); err != nil {
		t.Fatalf("Create worktree: %v", err)
	}
	runCleanupGit(t, s.Root, "branch", layout.Branch("TIC-888"))

	worktreeIDs, err := findOrphanWorktreeIDs(s)
	if err != nil {
		t.Fatalf("findOrphanWorktreeIDs: %v", err)
	}
	if len(worktreeIDs) != 1 || worktreeIDs[0] != "TIC-999" {
		t.Fatalf("worktree IDs = %v, want [TIC-999]", worktreeIDs)
	}

	branchIDs, err := findOrphanBranchIDs(s, layout)
	if err != nil {
		t.Fatalf("findOrphanBranchIDs: %v", err)
	}
	if len(branchIDs) != 1 || branchIDs[0] != layout.Branch("TIC-888") {
		t.Fatalf("branch IDs = %v, want [%s]", branchIDs, layout.Branch("TIC-888"))
	}
}

func TestCollectConfiguredStageActions(t *testing.T) {
	s := newCleanupStore(t)
	layout := worktreeLayout(s.Config)
	done, _ := s.Create("Done ticket")
	if _, err := s.Move(done.ID, "done"); err != nil {
		t.Fatalf("Move done: %v", err)
	}
	executeTk, _ := s.Create("Execute ticket")
	if _, err := s.Move(executeTk.ID, "execute"); err != nil {
		t.Fatalf("Move execute: %v", err)
	}

	if err := os.MkdirAll(agent.TicketDir(s.Root, done.ID), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := worktree.Create(s.Root, layout, done.ID, ""); err != nil {
		t.Fatalf("Create done worktree: %v", err)
	}
	if err := os.MkdirAll(agent.TicketDir(s.Root, executeTk.ID), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := worktree.Create(s.Root, layout, executeTk.ID, ""); err != nil {
		t.Fatalf("Create execute worktree: %v", err)
	}

	s.Config.Cleanup = &config.CleanupConfig{
		Stages: []config.CleanupStage{{
			Name:      "done",
			AgentData: true,
			Worktree:  true,
			Branch:    true,
		}},
	}

	actions, warnings, err := collectCleanupActions(s, cleanupOptions{})
	if err != nil {
		t.Fatalf("collectCleanupActions: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}

	var descriptions []string
	for _, action := range actions {
		descriptions = append(descriptions, action.Description)
	}

	want := []string{
		"remove agent data for " + done.ID + " in done",
		"remove worktree for " + done.ID + " in done",
		"delete branch " + layout.Branch(done.ID) + " for " + done.ID + " in done",
	}
	if strings.Join(descriptions, "\n") != strings.Join(want, "\n") {
		t.Fatalf("descriptions = %v\nwant %v", descriptions, want)
	}
}

func TestCollectConfiguredStageActionsSkipsActiveRuns(t *testing.T) {
	s := newCleanupStore(t)
	layout := worktreeLayout(s.Config)
	tk, _ := s.Create("Still running")
	if _, err := s.Move(tk.ID, "done"); err != nil {
		t.Fatalf("Move: %v", err)
	}
	if _, err := worktree.Create(s.Root, layout, tk.ID, ""); err != nil {
		t.Fatalf("Create worktree: %v", err)
	}
	if err := agent.Write(s.Root, agent.AgentStatus{
		TicketID:  tk.ID,
		RunID:     "001-execute",
		Seq:       1,
		Attempt:   1,
		Stage:     "execute",
		Agent:     "claude",
		Status:    agent.StatusRunning,
		SpawnedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("agent.Write: %v", err)
	}

	s.Config.Cleanup = &config.CleanupConfig{
		Stages: []config.CleanupStage{{
			Name:      "done",
			AgentData: true,
			Worktree:  true,
			Branch:    true,
		}},
	}

	actions, warnings, err := collectCleanupActions(s, cleanupOptions{})
	if err != nil {
		t.Fatalf("collectCleanupActions: %v", err)
	}
	if len(actions) != 0 {
		var descriptions []string
		for _, a := range actions {
			descriptions = append(descriptions, a.Description)
		}
		t.Fatalf("actions = %v, want none (active run should skip all artifacts)", descriptions)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], tk.ID) || !strings.Contains(warnings[0], "running") {
		t.Fatalf("warnings = %v, want one mentioning %s and running", warnings, tk.ID)
	}
}

func TestExecuteCleanupActionsDryRunMutatesNothing(t *testing.T) {
	s := newCleanupStore(t)
	layout := worktreeLayout(s.Config)
	tk, _ := s.Create("Done ticket")
	if _, err := s.Move(tk.ID, "done"); err != nil {
		t.Fatalf("Move: %v", err)
	}
	if err := os.MkdirAll(agent.TicketDir(s.Root, tk.ID), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := worktree.Create(s.Root, layout, tk.ID, ""); err != nil {
		t.Fatalf("Create worktree: %v", err)
	}

	s.Config.Cleanup = &config.CleanupConfig{
		Stages: []config.CleanupStage{{
			Name:      "done",
			AgentData: true,
			Worktree:  true,
			Branch:    true,
		}},
	}

	actions, warnings, err := collectCleanupActions(s, cleanupOptions{})
	if err != nil {
		t.Fatalf("collectCleanupActions: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}

	var out bytes.Buffer
	performed, failures := executeCleanupActions(&out, actions, true)
	if performed != 0 || failures != 0 {
		t.Fatalf("performed=%d failures=%d, want 0/0", performed, failures)
	}
	if !agentDataExists(s.Root, tk.ID) {
		t.Fatal("dry run removed agent data")
	}
	if !worktreeExists(s.Root, layout, tk.ID) {
		t.Fatal("dry run removed worktree")
	}
	if out.Len() == 0 || !strings.Contains(out.String(), "(dry run)") {
		t.Fatalf("output = %q, want dry run lines", out.String())
	}
	if out := runCleanupGit(t, s.Root, "branch", "--list", layout.Branch(tk.ID)); out == "" {
		t.Fatal("dry run deleted branch")
	}
}

func TestCollectCleanupActions_CustomLayout(t *testing.T) {
	s := newCleanupStoreWithConfig(t, config.Config{
		Prefix:        "TIC",
		ProjectPrefix: "PRJ",
		Stages:        []string{"backlog", "execute", "done"},
		Worktrees: &config.WorktreesConfig{
			Dir:          ".trees",
			BranchPrefix: "agent/",
		},
	})
	layout := worktreeLayout(s.Config)
	if _, err := worktree.Create(s.Root, layout, "TIC-999", ""); err != nil {
		t.Fatalf("Create worktree: %v", err)
	}
	runCleanupGit(t, s.Root, "branch", layout.Branch("TIC-888"))

	worktreeIDs, err := findOrphanWorktreeIDs(s)
	if err != nil {
		t.Fatalf("findOrphanWorktreeIDs: %v", err)
	}
	if len(worktreeIDs) != 1 || worktreeIDs[0] != "TIC-999" {
		t.Fatalf("worktree IDs = %v, want [TIC-999]", worktreeIDs)
	}

	branchIDs, err := findOrphanBranchIDs(s, layout)
	if err != nil {
		t.Fatalf("findOrphanBranchIDs: %v", err)
	}
	if len(branchIDs) != 1 || branchIDs[0] != "agent/TIC-888" {
		t.Fatalf("branch IDs = %v, want [agent/TIC-888]", branchIDs)
	}

	if _, err := os.Stat(filepath.Join(s.Root, ".trees", "TIC-999")); err != nil {
		t.Fatalf("custom worktree dir missing: %v", err)
	}
}
