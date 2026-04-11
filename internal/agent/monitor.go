package agent

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
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

// IdleChecker returns how many seconds a tmux pane has been idle.
// Returns -1 if the session doesn't exist or the query fails.
type IdleChecker func(sessionName string) int

// TmuxPaneIdle computes how long the pane has been idle by reading
// #{window_activity} (a Unix epoch) and subtracting from now.
func TmuxPaneIdle(sessionName string) int {
	out, err := exec.Command("tmux", "display-message", "-t", sessionName, "-p", "#{window_activity}").Output()
	if err != nil {
		return -1
	}
	epoch, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		return -1
	}
	return int(time.Now().Unix() - epoch)
}

// blockedThreshold is how long a pane must be idle (no output) before
// the monitor considers it blocked (likely waiting for user input).
const blockedThreshold = 30 // seconds

// Monitor periodically reconciles agent run files against tmux reality.
// It catches crashes, stale runs from a previous watcher process,
// promotes spawned → running, and detects blocked agents.
type Monitor struct {
	root      string
	interval  time.Duration
	check     SessionChecker
	idleCheck IdleChecker

	mu       sync.Mutex
	watching map[string]struct{} // "<ticket>/<run>" with active wait goroutines
}

// NewMonitor creates a monitor that checks tmux state every interval.
func NewMonitor(root string, check SessionChecker, idle IdleChecker) *Monitor {
	return &Monitor{
		root:      root,
		interval:  5 * time.Second,
		check:     check,
		idleCheck: idle,
		watching:  make(map[string]struct{}),
	}
}

func runKey(ticketID, runID string) string {
	return ticketID + "/" + runID
}

// TrackRun registers a (ticket, run) pair as having an active
// waitForTmuxSession goroutine. The monitor will not mark tracked
// runs as failed (that's the wait goroutine's job).
func (m *Monitor) TrackRun(ticketID, runID string) {
	m.mu.Lock()
	m.watching[runKey(ticketID, runID)] = struct{}{}
	m.mu.Unlock()
}

// UntrackRun removes a (ticket, run) pair from the tracked set.
func (m *Monitor) UntrackRun(ticketID, runID string) {
	m.mu.Lock()
	delete(m.watching, runKey(ticketID, runID))
	m.mu.Unlock()
}

func (m *Monitor) isTracked(ticketID, runID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.watching[runKey(ticketID, runID)]
	return ok
}

// AliveStatus is returned by Reconcile for sessions that are still
// running so the caller can re-attach wait goroutines.
type AliveStatus struct {
	AgentStatus
}

// Reconcile checks all non-terminal run files against tmux state.
// Sessions that are still alive are returned so the caller can spawn
// new wait goroutines for them. Sessions that are gone are marked
// failed.
func (m *Monitor) Reconcile() ([]AliveStatus, error) {
	statuses, err := ListAll(m.root)
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
					log.Printf("monitor: failed to promote %s/%s to running: %v", as.TicketID, as.RunID, err)
				}
			}
			alive = append(alive, AliveStatus{as})
		} else {
			as.Status = StatusFailed
			as.Error = "tmux session not found on watcher restart"
			if err := Write(m.root, as); err != nil {
				log.Printf("monitor: failed to mark %s/%s failed: %v", as.TicketID, as.RunID, err)
			} else {
				log.Printf("monitor: reconciled %s/%s as failed (stale status)", as.TicketID, as.RunID)
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
	statuses, err := ListAll(m.root)
	if err != nil {
		log.Printf("monitor: failed to list statuses: %v", err)
		return
	}

	// Track which tickets have any non-stale runs left so we can prune
	// fully-stale ticket directories at the end.
	ticketsWithLiveRuns := make(map[string]bool)

	for _, as := range statuses {
		if as.Status.IsTerminal() {
			if time.Since(as.UpdatedAt) <= staleAge {
				ticketsWithLiveRuns[as.TicketID] = true
			}
			continue
		}
		ticketsWithLiveRuns[as.TicketID] = true

		if m.check(as.Session) {
			idle := m.idleCheck(as.Session)

			switch as.Status {
			case StatusSpawned:
				as.Status = StatusRunning
				if err := Write(m.root, as); err != nil {
					log.Printf("monitor: failed to promote %s/%s to running: %v", as.TicketID, as.RunID, err)
				}
			case StatusRunning:
				if idle >= blockedThreshold {
					as.Status = StatusBlocked
					as.Error = fmt.Sprintf("pane idle for %ds, likely waiting for input", idle)
					if err := Write(m.root, as); err != nil {
						log.Printf("monitor: failed to mark %s/%s blocked: %v", as.TicketID, as.RunID, err)
					} else {
						log.Printf("monitor: %s/%s marked blocked (idle %ds)", as.TicketID, as.RunID, idle)
					}
				}
			case StatusBlocked:
				if idle < blockedThreshold {
					as.Status = StatusRunning
					as.Error = ""
					if err := Write(m.root, as); err != nil {
						log.Printf("monitor: failed to unblock %s/%s: %v", as.TicketID, as.RunID, err)
					} else {
						log.Printf("monitor: %s/%s resumed (no longer idle)", as.TicketID, as.RunID)
					}
				}
			}
		} else if !m.isTracked(as.TicketID, as.RunID) {
			as.Status = StatusFailed
			as.Error = "tmux session disappeared"
			if err := Write(m.root, as); err != nil {
				log.Printf("monitor: failed to mark %s/%s failed: %v", as.TicketID, as.RunID, err)
			} else {
				log.Printf("monitor: %s/%s marked failed (session gone, no watcher)", as.TicketID, as.RunID)
			}
		}
	}

	// Prune ticket directories whose every run is terminal and old.
	dirEntries, err := os.ReadDir(Dir(m.root))
	if err != nil {
		return
	}
	for _, e := range dirEntries {
		if !e.IsDir() {
			continue
		}
		if ticketsWithLiveRuns[e.Name()] {
			continue
		}
		if err := RemoveTicket(m.root, e.Name()); err != nil {
			log.Printf("monitor: failed to prune %s: %v", e.Name(), err)
		}
	}
}
