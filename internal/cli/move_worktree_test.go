package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stepandel/tickets-md/internal/config"
	"github.com/stepandel/tickets-md/internal/ticket"
)

func canonicalPath(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(resolved)
}

func runMoveGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func newMoveWorktreeRepo(t *testing.T) (string, *ticket.Store, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	root := t.TempDir()
	runMoveGit(t, root, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("init\n"), 0o644); err != nil {
		t.Fatalf("WriteFile README: %v", err)
	}
	runMoveGit(t, root, "add", "README.md")
	runMoveGit(t, root, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-m", "init")

	s, err := ticket.Init(root, config.Config{
		Prefix:        "TIC",
		ProjectPrefix: "PRJ",
		Stages:        []string{"backlog", "execute", "review", "done"},
	})
	if err != nil {
		t.Fatalf("Init store: %v", err)
	}
	runMoveGit(t, root, "add", ".gitignore", ".tickets/config.yml", ".tickets/backlog/.stage.yml", ".tickets/execute/.stage.yml", ".tickets/review/.stage.yml", ".tickets/done/.stage.yml")
	runMoveGit(t, root, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-m", "add store")

	tk, err := s.Create("Execute ticket")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := s.Move(tk.ID, "execute"); err != nil {
		t.Fatalf("Move to execute: %v", err)
	}

	worktreePath := filepath.Join(t.TempDir(), "wt")
	runMoveGit(t, root, "worktree", "add", worktreePath)
	return root, s, worktreePath
}

func chdirForTest(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir(%s): %v", dir, err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(prev); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})
}

func resetRootFlags(t *testing.T) {
	t.Helper()
	prev := globalFlags
	globalFlags = rootFlags{}
	t.Cleanup(func() {
		globalFlags = prev
	})
}

func TestMoveCommandRedirectsFromLinkedWorktree(t *testing.T) {
	_, s, worktreePath := newMoveWorktreeRepo(t)
	resetRootFlags(t)
	chdirForTest(t, worktreePath)

	cmd := NewRootCmd()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"move", "TIC-001", "review"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	got, err := s.Get("TIC-001")
	if err != nil {
		t.Fatalf("Get moved ticket: %v", err)
	}
	if got.Stage != "review" {
		t.Fatalf("Stage = %q, want review", got.Stage)
	}
	if !strings.Contains(stdout.String(), "Moved TIC-001 -> review") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "Using main repo ticket store at "+canonicalPath(t, s.Root)) {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestMoveCommandExplicitRootDoesNotRedirectFromLinkedWorktree(t *testing.T) {
	_, s, worktreePath := newMoveWorktreeRepo(t)
	resetRootFlags(t)
	chdirForTest(t, worktreePath)

	cmd := NewRootCmd()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"-C", ".", "move", "TIC-001", "review"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "ticket not found: TIC-001") {
		t.Fatalf("error = %v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "Error: ticket not found: TIC-001") {
		t.Fatalf("stderr = %q", stderr.String())
	}

	got, err := s.Get("TIC-001")
	if err != nil {
		t.Fatalf("Get ticket after failed move: %v", err)
	}
	if got.Stage != "execute" {
		t.Fatalf("Stage = %q, want execute", got.Stage)
	}
}

func TestMoveCommandFromMainRepoDoesNotRedirect(t *testing.T) {
	root, s, _ := newMoveWorktreeRepo(t)
	resetRootFlags(t)
	chdirForTest(t, root)

	cmd := NewRootCmd()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"move", "TIC-001", "review"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	got, err := s.Get("TIC-001")
	if err != nil {
		t.Fatalf("Get moved ticket: %v", err)
	}
	if got.Stage != "review" {
		t.Fatalf("Stage = %q, want review", got.Stage)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestMoveCommandLinkedWorktreeFallsBackWhenMainRepoHasNoStore(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	root := t.TempDir()
	runMoveGit(t, root, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("init\n"), 0o644); err != nil {
		t.Fatalf("WriteFile README: %v", err)
	}
	runMoveGit(t, root, "add", "README.md")
	runMoveGit(t, root, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-m", "init")

	worktreePath := filepath.Join(t.TempDir(), "wt")
	runMoveGit(t, root, "worktree", "add", worktreePath)

	resetRootFlags(t)
	chdirForTest(t, worktreePath)

	cmd := NewRootCmd()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"move", "TIC-001", "review"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "no ticket store here") {
		t.Fatalf("error = %v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "Error: no ticket store here") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}
