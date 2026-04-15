package worktree

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func newGitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	runGit(t, root, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("init\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	runGit(t, root, "add", "README.md")
	runGit(t, root, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-m", "init")
	return root
}

func TestCreate_NewBranch(t *testing.T) {
	root := newGitRepo(t)

	got, err := Create(root, "TIC-001", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	want := filepath.Join(root, Dir, "TIC-001")
	if got != want {
		t.Fatalf("Create() = %q, want %q", got, want)
	}
	info, err := os.Stat(got)
	if err != nil || !info.IsDir() {
		t.Fatalf("worktree dir missing: %v", err)
	}
	if out := runGit(t, root, "branch", "--list", BranchPrefix+"TIC-001"); out == "" {
		t.Fatal("expected worktree branch to exist")
	}
}

func TestCreate_Idempotent(t *testing.T) {
	root := newGitRepo(t)

	first, err := Create(root, "TIC-001", "")
	if err != nil {
		t.Fatalf("Create first: %v", err)
	}
	second, err := Create(root, "TIC-001", "")
	if err != nil {
		t.Fatalf("Create second: %v", err)
	}
	if second != first {
		t.Fatalf("Create second = %q, want %q", second, first)
	}
}

func TestCreate_BranchAlreadyExists(t *testing.T) {
	root := newGitRepo(t)
	runGit(t, root, "branch", BranchPrefix+"TIC-002")

	got, err := Create(root, "TIC-002", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := os.Stat(got); err != nil {
		t.Fatalf("worktree dir missing: %v", err)
	}
}

func TestCreate_BaseBranch(t *testing.T) {
	root := newGitRepo(t)
	runGit(t, root, "checkout", "-b", "feature")
	if err := os.WriteFile(filepath.Join(root, "feature.txt"), []byte("feature\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	runGit(t, root, "add", "feature.txt")
	runGit(t, root, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-m", "feature")
	featureCommit := runGit(t, root, "rev-parse", "feature")
	runGit(t, root, "checkout", "main")

	wtPath, err := Create(root, "TIC-003", "feature")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got := runGit(t, wtPath, "rev-parse", "HEAD"); got != featureCommit {
		t.Fatalf("worktree HEAD = %q, want %q", got, featureCommit)
	}
}

func TestList_Empty(t *testing.T) {
	root := newGitRepo(t)

	got, err := List(root)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got != nil {
		t.Fatalf("List() = %#v, want nil", got)
	}
}

func TestList_AfterCreate(t *testing.T) {
	root := newGitRepo(t)
	wt1, err := Create(root, "TIC-001", "")
	if err != nil {
		t.Fatalf("Create TIC-001: %v", err)
	}
	wt2, err := Create(root, "TIC-002", "")
	if err != nil {
		t.Fatalf("Create TIC-002: %v", err)
	}

	got, err := List(root)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(List()) = %d, want 2", len(got))
	}

	seen := map[string]string{}
	for _, info := range got {
		seen[info.Path] = info.Branch
	}
	if seen[wt1] != BranchPrefix+"TIC-001" {
		t.Fatalf("branch for %q = %q", wt1, seen[wt1])
	}
	if seen[wt2] != BranchPrefix+"TIC-002" {
		t.Fatalf("branch for %q = %q", wt2, seen[wt2])
	}
}

func TestRemove(t *testing.T) {
	root := newGitRepo(t)
	wtPath, err := Create(root, "TIC-001", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := Remove(root, "TIC-001"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Fatalf("worktree still exists, stat err = %v", err)
	}
	if out := runGit(t, root, "worktree", "list"); strings.Contains(out, wtPath) {
		t.Fatalf("git worktree list still contains %q:\n%s", wtPath, out)
	}
}

func TestDeleteBranch_NoOpWhenMissing(t *testing.T) {
	root := newGitRepo(t)
	if err := DeleteBranch(root, "TIC-404"); err != nil {
		t.Fatalf("DeleteBranch: %v", err)
	}
}

func TestDeleteBranch_Success(t *testing.T) {
	root := newGitRepo(t)
	if _, err := Create(root, "TIC-001", ""); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := Remove(root, "TIC-001"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if err := DeleteBranch(root, "TIC-001"); err != nil {
		t.Fatalf("DeleteBranch: %v", err)
	}
	if out := runGit(t, root, "branch", "--list", BranchPrefix+"TIC-001"); out != "" {
		t.Fatalf("branch still exists: %q", out)
	}
}

func TestEnsureGitignored(t *testing.T) {
	root := newGitRepo(t)

	if err := EnsureGitignored(root); err != nil {
		t.Fatalf("EnsureGitignored first: %v", err)
	}
	if err := EnsureGitignored(root); err != nil {
		t.Fatalf("EnsureGitignored second: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != ".worktrees\n" {
		t.Fatalf("gitignore = %q, want %q", string(data), ".worktrees\n")
	}

	root2 := newGitRepo(t)
	if err := os.WriteFile(filepath.Join(root2, ".gitignore"), []byte("node_modules"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := EnsureGitignored(root2); err != nil {
		t.Fatalf("EnsureGitignored third: %v", err)
	}
	data, err = os.ReadFile(filepath.Join(root2, ".gitignore"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "node_modules\n.worktrees\n" {
		t.Fatalf("gitignore = %q, want newline-separated entry", string(data))
	}
}
