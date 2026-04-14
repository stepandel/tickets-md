package agent

import "testing"

func TestTransitionAllowedEdges(t *testing.T) {
	allowed := []struct{ from, to Status }{
		{StatusSpawned, StatusRunning},
		{StatusSpawned, StatusErrored},
		{StatusSpawned, StatusFailed},
		{StatusRunning, StatusDone},
		{StatusRunning, StatusFailed},
		{StatusRunning, StatusBlocked},
		{StatusBlocked, StatusRunning},
		{StatusBlocked, StatusDone},
		{StatusBlocked, StatusFailed},
	}
	for _, tc := range allowed {
		if err := Transition(tc.from, tc.to); err != nil {
			t.Errorf("Transition(%q → %q) unexpectedly rejected: %v", tc.from, tc.to, err)
		}
	}
}

func TestTransitionRejectsFromTerminal(t *testing.T) {
	for _, from := range []Status{StatusDone, StatusFailed, StatusErrored} {
		for _, to := range []Status{StatusSpawned, StatusRunning, StatusBlocked, StatusDone, StatusFailed, StatusErrored} {
			if err := Transition(from, to); err == nil {
				t.Errorf("Transition(%q → %q) should be rejected — %q is terminal", from, to, from)
			}
		}
	}
}

func TestTransitionRejectsInvalidEdges(t *testing.T) {
	invalid := []struct{ from, to Status }{
		{StatusSpawned, StatusDone},    // must go through running
		{StatusSpawned, StatusBlocked}, // can't skip to blocked
		{StatusRunning, StatusSpawned}, // can't regress
		{StatusRunning, StatusErrored}, // errored is only for spawn failures
		{StatusBlocked, StatusSpawned}, // can't regress
	}
	for _, tc := range invalid {
		if err := Transition(tc.from, tc.to); err == nil {
			t.Errorf("Transition(%q → %q) should be rejected", tc.from, tc.to)
		}
	}
}

func TestIsTerminal(t *testing.T) {
	terminal := []Status{StatusDone, StatusFailed, StatusErrored}
	for _, s := range terminal {
		if !s.IsTerminal() {
			t.Errorf("%q should be terminal", s)
		}
	}
	nonTerminal := []Status{StatusSpawned, StatusRunning, StatusBlocked}
	for _, s := range nonTerminal {
		if s.IsTerminal() {
			t.Errorf("%q should not be terminal", s)
		}
	}
}
