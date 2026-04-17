package cli

import "testing"

func TestForceRerunPrompt(t *testing.T) {
	got := forceRerunPrompt("TIC-097", "TIC-097-3")
	want := "Force re-run stage agent for TIC-097? This will kill active session TIC-097-3."
	if got != want {
		t.Fatalf("forceRerunPrompt() = %q, want %q", got, want)
	}
}
