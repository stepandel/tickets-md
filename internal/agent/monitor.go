package agent

import (
	"context"
	"log"
	"os"
	"os/exec"
	"sync"
	"time"
)

// SessionChecker reports whether a tmux session is alive. Extracted as
// a function type so tests can substitute a fake.
type SessionChecker func(sessionName string) bool

// TmuxSessionExists is the production implementation of SessionChecker.
func TmuxSessionExists(sessionName string) bool {
	return exec.Command("tmux", "has-session", "-t", sessionName).Run() == nil
}

// Monitor periodically reconciles agent status files against tmux
// reality. It catches crashes, stale statuses from a previous watcher
// run, and promotes spawned → running.
type Monitor struct {
	root     string
	interval time.Duration
	check    SessionChecker

	mu       sync.Mutex
	watching map[string]struct{} // ticket IDs with active wait goroutines
}

// NewMonitor creates a monitor that checks tmux state every interval.
func NewMonitor(root string, check SessionChecker) *Monitor {
	return &Monitor{
		root:     root,
		interval: 5 * time.Second,
		check:    check,
		watching: make(map[string]struct{}),
	}
}

// TrackSession registers a ticket ID as having an active
// waitForTmuxSession goroutine. The monitor will not mark tracked
// sessions as failed (that's the wait goroutine's job).
func (m *Monitor) TrackSession(ticketID string) {
	m.mu.Lock()
	m.watching[ticketID] = struct{}{}
	m.mu.Unlock()
}

// UntrackSession removes a ticket ID from the tracked set.
func (m *Monitor) UntrackSession(ticketID string) {
	m.mu.Lock()
	delete(m.watching, ticketID)
	m.mu.Unlock()
}

func (m *Monitor) isTracked(ticketID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.watching[ticketID]
	return ok
}

// AliveStatus is returned by Reconcile for sessions that are still
// running so the caller can re-attach wait goroutines.
type AliveStatus struct {
	AgentStatus
}

// Reconcile checks all non-terminal status files against tmux state.
// Sessions that are still alive are returned so the caller can spawn
// new wait goroutines for them. Sessions that are gone are marked
// failed.
func (m *Monitor) Reconcile() ([]AliveStatus, error) {
	statuses, err := List(m.root)
	if err != nil {
		return nil, err
	}
	var alive []AliveStatus
	for _, as := range statuses {
		if as.Status.IsTerminal() {
			continue
		}
		if m.check(as.Session) {
			if as.Status == StatusSpawned {
				as.Status = StatusRunning
				if err := Write(m.root, as); err != nil {
					log.Printf("monitor: failed to promote %s to running: %v", as.TicketID, err)
				}
			}
			alive = append(alive, AliveStatus{as})
		} else {
			as.Status = StatusFailed
			as.Error = "tmux session not found on watcher restart"
			if err := Write(m.root, as); err != nil {
				log.Printf("monitor: failed to mark %s as failed: %v", as.TicketID, err)
			} else {
				log.Printf("monitor: reconciled %s as failed (stale status)", as.TicketID)
			}
		}
	}
	return alive, nil
}

// Run starts the periodic poll loop. It blocks until ctx is cancelled.
// Call Reconcile separately before Run if you need to handle alive
// sessions on startup.
func (m *Monitor) Run(ctx context.Context) {
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.poll()
		}
	}
}

const staleAge = 24 * time.Hour

func (m *Monitor) poll() {
	statuses, err := List(m.root)
	if err != nil {
		log.Printf("monitor: failed to list statuses: %v", err)
		return
	}
	for _, as := range statuses {
		// Auto-cleanup old terminal statuses.
		if as.Status.IsTerminal() {
			if time.Since(as.UpdatedAt) > staleAge {
				os.Remove(statusPath(m.root, as.TicketID))
			}
			continue
		}

		if m.check(as.Session) {
			// Session alive — promote spawned → running.
			if as.Status == StatusSpawned {
				as.Status = StatusRunning
				if err := Write(m.root, as); err != nil {
					log.Printf("monitor: failed to promote %s to running: %v", as.TicketID, err)
				}
			}
		} else if !m.isTracked(as.TicketID) {
			// Session gone and no wait goroutine watching it — mark failed.
			as.Status = StatusFailed
			as.Error = "tmux session disappeared"
			if err := Write(m.root, as); err != nil {
				log.Printf("monitor: failed to mark %s as failed: %v", as.TicketID, err)
			} else {
				log.Printf("monitor: %s marked failed (session gone, no watcher)", as.TicketID)
			}
		}
		// Session gone but tracked — the wait goroutine will handle it.
	}
}
