package agent

import (
	"bytes"
	"strings"
	"testing"
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

func TestTrimReplay_FallsBackToEscBoundary(t *testing.T) {
	// No reset sequence anywhere — but there's a cursor-position
	// escape past the cut point. trimReplay must start at that ESC,
	// never slice it in half.
	prefix := strings.Repeat("A", maxReplayBytes+100)
	buf := append([]byte(prefix), []byte("\x1b[42;2Htail")...)

	got := trimReplay(buf)
	if got[0] != 0x1b {
		t.Fatalf("expected trim to start at ESC, got 0x%02x (%q)", got[0], got[:min(len(got), 16)])
	}
}

func TestTrimReplay_NoEscapeSequencesTrimsByBytes(t *testing.T) {
	// Pure text, no escapes — just enforce the cap.
	buf := bytes.Repeat([]byte("x"), maxReplayBytes+500)
	got := trimReplay(buf)
	if len(got) != maxReplayBytes {
		t.Fatalf("expected len %d, got %d", maxReplayBytes, len(got))
	}
}
