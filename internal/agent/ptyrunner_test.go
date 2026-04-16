package agent

import (
	"bytes"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestTrimReplay_UnderCapUntouched(t *testing.T) {
	buf := []byte("hello world")
	got := trimReplay(buf)
	if !bytes.Equal(got, buf) {
		t.Fatalf("under-cap buffer modified: %q", got)
	}
}

func TestTrimReplay_CutsAtResetSequence(t *testing.T) {
	prefix := strings.Repeat("A", maxReplayBytes)
	// Place a clear-screen shortly after the cut point; trimReplay
	// should discard everything before it so the client replays from
	// a clean frame boundary.
	buf := append([]byte(prefix), []byte("XX\x1b[2JAFTER")...)

	got := trimReplay(buf)
	if !bytes.HasPrefix(got, []byte("\x1b[2JAFTER")) {
		t.Fatalf("expected trim to start at \\x1b[2J, got %q", got)
	}
}

func TestTrimReplay_FallsBackToSyntheticReset(t *testing.T) {
	// No reset sequence in the retained window — but there's a
	// cursor-position escape past the cut point. trimReplay must
	// prepend a synthetic reset and resume at that ESC, never slicing
	// it in half.
	prefix := strings.Repeat("A", maxReplayBytes+100)
	buf := append([]byte(prefix), []byte("\x1b[42;2Htail")...)

	got := trimReplay(buf)
	if !bytes.HasPrefix(got, syntheticResetPrefix) {
		t.Fatalf("expected synthetic reset prefix, got %q", got[:min(len(got), 40)])
	}
	if !bytes.HasSuffix(got, []byte("\x1b[42;2Htail")) {
		t.Fatalf("expected cut to resume at ESC, got tail %q", got[max(0, len(got)-16):])
	}
}

func TestTrimReplay_NoEscapeSequencesSynthesizesReset(t *testing.T) {
	// Pure text, no escapes — still needs a clean start, so we expect
	// the synthetic reset prefix followed by the tail.
	buf := bytes.Repeat([]byte("x"), maxReplayBytes+500)
	got := trimReplay(buf)
	if !bytes.HasPrefix(got, syntheticResetPrefix) {
		t.Fatalf("expected synthetic reset prefix, got %q", got[:min(len(got), 40)])
	}
	expected := len(syntheticResetPrefix) + maxReplayBytes
	if len(got) != expected {
		t.Fatalf("expected len %d, got %d", expected, len(got))
	}
}

func TestPTYRunnerWaitReceivesFastExitCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		argv []string
		want int
	}{
		{
			name: "zero",
			argv: []string{"/bin/sh", "-c", "exit 0"},
			want: 0,
		},
		{
			name: "nonzero",
			argv: []string{"/bin/sh", "-c", "exit 127"},
			want: 127,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			runner := NewPTYRunner()
			session := tc.name
			logPath := filepath.Join(t.TempDir(), "session.log")
			if err := runner.Start(session, t.TempDir(), tc.argv, logPath, 0, 0); err != nil {
				t.Fatalf("Start: %v", err)
			}

			waitForSessionExit(t, runner, session)

			code, err := runner.Wait(session)
			if err != nil {
				t.Fatalf("Wait: %v", err)
			}
			if code == nil {
				t.Fatal("Wait returned nil exit code")
			}
			if *code != tc.want {
				t.Fatalf("Wait exit code = %d, want %d", *code, tc.want)
			}
			if runner.Alive(session) {
				t.Fatalf("Alive(%q) = true after Wait", session)
			}
		})
	}
}

// TestPTYRunnerShutdownDoesNotStealExitFromConcurrentWaiter exercises a
// goroutine-scheduling race: under the old implementation, either
// Shutdown or the owning Wait could delete the map entry first. This
// test is therefore probabilistic — rely on `-race -count=N` (the
// ticket's verification uses count=20) to surface regressions; a single
// run may pass against buggy code by luck.
func TestPTYRunnerShutdownDoesNotStealExitFromConcurrentWaiter(t *testing.T) {
	t.Parallel()

	runner := NewPTYRunner()
	session := "shutdown-race"
	logPath := filepath.Join(t.TempDir(), "session.log")
	if err := runner.Start(session, t.TempDir(), []string{"/bin/sh", "-c", "exit 0"}, logPath, 0, 0); err != nil {
		t.Fatalf("Start: %v", err)
	}

	waitForSessionExit(t, runner, session)

	var (
		wg       sync.WaitGroup
		exitCode *int
		waitErr  error
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		exitCode, waitErr = runner.Wait(session)
	}()

	runner.Shutdown()
	wg.Wait()

	if waitErr != nil {
		t.Fatalf("Wait: %v", waitErr)
	}
	if exitCode == nil {
		t.Fatal("Wait returned nil exit code")
	}
	if *exitCode != 0 {
		t.Fatalf("Wait exit code = %d, want 0", *exitCode)
	}
}

func waitForSessionExit(t *testing.T, runner *PTYRunner, name string) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	if testDeadline, ok := t.Deadline(); ok && testDeadline.Before(deadline) {
		deadline = testDeadline
	}

	for time.Now().Before(deadline) {
		if !runner.Alive(name) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("session %q stayed alive until deadline", name)
}
