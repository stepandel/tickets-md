package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestWatchLockHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_WATCH_LOCK_HELPER") != "1" {
		return
	}

	root := os.Getenv("GO_WATCH_LOCK_ROOT")
	if root == "" {
		fmt.Fprintln(os.Stderr, "missing GO_WATCH_LOCK_ROOT")
		os.Exit(2)
	}

	lock, err := acquireWatcherLock(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "acquireWatcherLock: %v\n", err)
		os.Exit(1)
	}
	defer lock.Release()

	fmt.Println("ready")
	if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
		fmt.Fprintf(os.Stderr, "stdin: %v\n", err)
		os.Exit(1)
	}
	os.Exit(0)
}

func TestAcquireWatcherLockAfterReleaseSucceeds(t *testing.T) {
	s := newWatchStore(t)

	lock, err := acquireWatcherLock(s.Root)
	if err != nil {
		t.Fatalf("first acquireWatcherLock: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("first Release: %v", err)
	}

	lock, err = acquireWatcherLock(s.Root)
	if err != nil {
		t.Fatalf("second acquireWatcherLock: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("second Release: %v", err)
	}
}

func TestAcquireWatcherLockDuplicateProcess(t *testing.T) {
	s := newWatchStore(t)
	proc := startWatchLockHelper(t, s.Root)
	defer proc.close(t)

	_, err := acquireWatcherLock(s.Root)
	if err == nil {
		t.Fatal("acquireWatcherLock succeeded, want duplicate error")
	}

	var dupErr *duplicateWatcherError
	if !errors.As(err, &dupErr) {
		t.Fatalf("error = %v, want duplicateWatcherError", err)
	}
	if dupErr.info.PID != proc.cmd.Process.Pid {
		t.Fatalf("duplicate pid = %d, want %d", dupErr.info.PID, proc.cmd.Process.Pid)
	}
	if dupErr.info.Hostname == "" {
		t.Fatal("duplicate hostname empty")
	}
	if dupErr.info.StartedAt.IsZero() {
		t.Fatal("duplicate started_at zero")
	}
	if dupErr.path != watcherLockPath(s.Root) {
		t.Fatalf("duplicate path = %q, want %q", dupErr.path, watcherLockPath(s.Root))
	}
	if !strings.Contains(err.Error(), fmt.Sprintf("kill %d", proc.cmd.Process.Pid)) {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestAcquireWatcherLockSurvivesStaleFile(t *testing.T) {
	s := newWatchStore(t)
	path := watcherLockPath(s.Root)
	if err := os.WriteFile(path, []byte(`{"pid":999999,"hostname":"stale","started_at":"2000-01-01T00:00:00Z"}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	lock, err := acquireWatcherLock(s.Root)
	if err != nil {
		t.Fatalf("acquireWatcherLock: %v", err)
	}
	defer lock.Release()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var info watcherLockInfo
	if err := json.Unmarshal(data, &info); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if info.PID != os.Getpid() {
		t.Fatalf("pid = %d, want %d", info.PID, os.Getpid())
	}
	if info.Hostname == "stale" {
		t.Fatalf("hostname = %q, want rewritten value", info.Hostname)
	}
}

func TestWatchCmdRefusesWhenLockHeld(t *testing.T) {
	s := newWatchStore(t)
	proc := startWatchLockHelper(t, s.Root)
	defer proc.close(t)

	resetRootFlags(t)
	chdirForTest(t, s.Root)
	globalFlags.root = "."

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
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(err.Error(), "another watcher is already running for this repo") {
		t.Fatalf("error = %v", err)
	}
	if !strings.Contains(err.Error(), fmt.Sprintf("kill %d", proc.cmd.Process.Pid)) {
		t.Fatalf("error = %v", err)
	}
	if !strings.Contains(err.Error(), ".tickets/.watch.lock") {
		t.Fatalf("error = %v", err)
	}
	if !strings.Contains(stderr.String(), "another watcher is already running for this repo") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

type watchLockHelper struct {
	cmd   *exec.Cmd
	stdin io.Closer
}

func startWatchLockHelper(t *testing.T, root string) *watchLockHelper {
	t.Helper()

	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("Executable: %v", err)
	}

	cmd := exec.Command(exe, "-test.run", "^TestWatchLockHelperProcess$")
	cmd.Env = append(os.Environ(),
		"GO_WANT_WATCH_LOCK_HELPER=1",
		"GO_WATCH_LOCK_ROOT="+root,
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe: %v", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatalf("StderrPipe: %v", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("StdinPipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start helper: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		buf := make([]byte, len("ready\n"))
		_, err := io.ReadFull(stdout, buf)
		if err != nil {
			stderrBytes, _ := io.ReadAll(stderr)
			done <- fmt.Errorf("helper handshake: %w (%s)", err, strings.TrimSpace(string(stderrBytes)))
			return
		}
		if string(buf) != "ready\n" {
			stderrBytes, _ := io.ReadAll(stderr)
			done <- fmt.Errorf("helper handshake = %q (%s)", string(buf), strings.TrimSpace(string(stderrBytes)))
			return
		}
		done <- nil
	}()

	select {
	case err := <-done:
		if err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatal("helper timeout")
	}

	return &watchLockHelper{cmd: cmd, stdin: stdin}
}

func (h *watchLockHelper) close(t *testing.T) {
	t.Helper()
	if h == nil || h.cmd == nil {
		return
	}
	if h.stdin != nil {
		_ = h.stdin.Close()
	}
	if err := h.cmd.Wait(); err != nil {
		t.Fatalf("helper Wait: %v", err)
	}
}
