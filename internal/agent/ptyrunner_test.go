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
