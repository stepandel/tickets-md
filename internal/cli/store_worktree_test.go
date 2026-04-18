package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stepandel/tickets-md/internal/config"
	"github.com/stepandel/tickets-md/internal/ticket"
)

func TestShowCommandRedirectsFromLinkedWorktree(t *testing.T) {
	_, s, worktreePath := newMoveWorktreeRepo(t)
	resetRootFlags(t)
	chdirForTest(t, worktreePath)

	cmd := NewRootCmd()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"show", "TIC-001"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !strings.Contains(stderr.String(), "Using main repo ticket store at "+canonicalPath(t, s.Root)) {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestSetCommandRedirectsFromLinkedWorktree(t *testing.T) {
	_, s, worktreePath := newMoveWorktreeRepo(t)
	resetRootFlags(t)
	chdirForTest(t, worktreePath)

	cmd := NewRootCmd()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"set", "TIC-001", "priority", "high"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	got, err := s.Get("TIC-001")
	if err != nil {
		t.Fatalf("Get updated ticket: %v", err)
	}
	if got.Priority != "high" {
		t.Fatalf("Priority = %q, want high", got.Priority)
	}
	if !strings.Contains(stderr.String(), "Using main repo ticket store at "+canonicalPath(t, s.Root)) {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestNewCommandRedirectsFromLinkedWorktree(t *testing.T) {
	root, s, worktreePath := newMoveWorktreeRepo(t)
	resetRootFlags(t)
	chdirForTest(t, worktreePath)

	cmd := NewRootCmd()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"new", "Created", "from", "worktree"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	got, err := s.Get("TIC-002")
	if err != nil {
		t.Fatalf("Get created ticket: %v", err)
	}
	if got.Title != "Created from worktree" {
		t.Fatalf("Title = %q", got.Title)
	}
	if !strings.Contains(stderr.String(), "Using main repo ticket store at "+canonicalPath(t, s.Root)) {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(worktreePath, ".tickets", "backlog", "TIC-002.md")); !os.IsNotExist(err) {
		t.Fatalf("worktree store unexpectedly has created ticket, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".tickets", "backlog", "TIC-002.md")); err != nil {
		t.Fatalf("main store missing created ticket: %v", err)
	}
}

func TestExplicitRootSkipsRedirectAcrossCommands(t *testing.T) {
	root, s, worktreePath := newMoveWorktreeRepo(t)
	failingTests := []struct {
		name string
		args []string
	}{
		{name: "show", args: []string{"-C", ".", "show", "TIC-001"}},
		{name: "set", args: []string{"-C", ".", "set", "TIC-001", "priority", "high"}},
	}

	for _, tc := range failingTests {
		t.Run(tc.name, func(t *testing.T) {
			resetRootFlags(t)
			chdirForTest(t, worktreePath)

			cmd := NewRootCmd()
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)
			cmd.SetArgs(tc.args)
			err := cmd.Execute()
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), "ticket not found") {
				t.Fatalf("error = %v", err)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
			if strings.Contains(stderr.String(), "Using main repo ticket store at ") {
				t.Fatalf("stderr = %q", stderr.String())
			}
		})
	}

	t.Run("new", func(t *testing.T) {
		resetRootFlags(t)
		chdirForTest(t, worktreePath)

		cmd := NewRootCmd()
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		cmd.SetOut(&stdout)
		cmd.SetErr(&stderr)
		cmd.SetArgs([]string{"-C", ".", "new", "Created", "from", "worktree"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if strings.Contains(stderr.String(), "Using main repo ticket store at ") {
			t.Fatalf("stderr = %q", stderr.String())
		}
		if _, err := os.Stat(filepath.Join(root, ".tickets", "backlog", "TIC-002.md")); !os.IsNotExist(err) {
			t.Fatalf("main store unexpectedly has TIC-002, err=%v", err)
		}
		if _, err := os.Stat(filepath.Join(worktreePath, ".tickets", "backlog", "TIC-001.md")); err != nil {
			t.Fatalf("worktree store missing TIC-001: %v", err)
		}
	})

	got, err := s.Get("TIC-001")
	if err != nil {
		t.Fatalf("Get ticket after explicit-root commands: %v", err)
	}
	if got.Priority != "" {
		t.Fatalf("Priority = %q, want empty", got.Priority)
	}
}

func TestWatchCommandStaysOnPlainOpenStore(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(file), "watch.go"))
	if err != nil {
		t.Fatalf("ReadFile watch.go: %v", err)
	}
	src := string(data)
	if !strings.Contains(src, "s, err := openStore()") {
		t.Fatalf("watch.go no longer uses openStore()")
	}
	if strings.Contains(src, "openStoreAuto(cmd)") {
		t.Fatalf("watch.go unexpectedly uses openStoreAuto")
	}
}

func TestWatchCommandRefusesFromLinkedWorktree(t *testing.T) {
	_, s, worktreePath := newMoveWorktreeRepo(t)
	resetRootFlags(t)
	chdirForTest(t, worktreePath)

	cmd := NewRootCmd()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"watch"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "refusing to start from a linked git worktree") {
		t.Fatalf("error = %v", err)
	}
	if !strings.Contains(err.Error(), canonicalPath(t, s.Root)) {
		t.Fatalf("error = %v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

func TestPreflightWatchRootAllowsExplicitRootInLinkedWorktree(t *testing.T) {
	_, _, worktreePath := newMoveWorktreeRepo(t)
	resetRootFlags(t)
	chdirForTest(t, worktreePath)

	globalFlags.root = "."
	globalFlags.rootExplicit = true

	if err := preflightWatchRoot(); err != nil {
		t.Fatalf("preflightWatchRoot: %v", err)
	}
}

func TestPreflightWatchRootAllowsMainRepo(t *testing.T) {
	root, _, _ := newMoveWorktreeRepo(t)
	resetRootFlags(t)
	chdirForTest(t, root)

	globalFlags.root = "."

	if err := preflightWatchRoot(); err != nil {
		t.Fatalf("preflightWatchRoot: %v", err)
	}
}

func TestPreflightWatchRootAllowsNonGitDir(t *testing.T) {
	root := t.TempDir()
	if _, err := ticket.Init(root, config.Config{
		Prefix:        "TIC",
		ProjectPrefix: "PRJ",
		Stages:        []string{"backlog", "execute", "done"},
	}); err != nil {
		t.Fatalf("Init: %v", err)
	}

	resetRootFlags(t)
	chdirForTest(t, root)
	globalFlags.root = "."

	if err := preflightWatchRoot(); err != nil {
		t.Fatalf("preflightWatchRoot: %v", err)
	}
}
