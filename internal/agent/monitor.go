package agent

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"
)

// SessionChecker reports whether a session is alive. Extracted as a
// function type so tests can substitute a fake. The production
// implementation is PTYRunner.Alive.
type SessionChecker func(sessionName string) bool

// IdleChecker returns how many seconds a session has been idle (no
// output). Returns -1 if the session doesn't exist. The production
// implementation is PTYRunner.IdleSeconds.
type IdleChecker func(sessionName string) int

// SessionKiller terminates a live session by name. The production
// implementation is PTYRunner.Kill.
type SessionKiller func(sessionName string) error

const (
	DefaultPollInterval = 5 * time.Second
	DefaultBlockedIdle  = 30 * time.Second
)

// Monitor periodically reconciles agent run files against session
// reality. It catches crashes, stale runs from a previous watcher
// process, promotes spawned → running, and detects blocked agents.
type Monitor struct {
	root      string
	check     SessionChecker
	idleCheck IdleChecker
	kill      SessionKiller

	// OnStatusChange fires after the monitor successfully writes a run
	// YAML. The watcher uses it to resync the ticket's frontmatter from
	// the latest run. Optional; nil means no callback.
	OnStatusChange func(ticketID string)

	configMu      sync.RWMutex
	interval      time.Duration
	blockedIdle   time.Duration
	idleKillAfter time.Duration
	configReload  chan struct{}

	mu       sync.Mutex
	watching map[string]struct{} // "<ticket>/<run>" with active wait goroutines
}

// NewMonitor creates a monitor that checks session state every interval.
func NewMonitor(root string, interval, blockedIdle, idleKillAfter time.Duration, check SessionChecker, idle IdleChecker, kill SessionKiller) *Monitor {
	interval, blockedIdle, idleKillAfter = normalizeTiming(interval, blockedIdle, idleKillAfter)
	return &Monitor{
		root:          root,
		interval:      interval,
		blockedIdle:   blockedIdle,
		idleKillAfter: idleKillAfter,
		check:         check,
		idleCheck:     idle,
		kill:          kill,
		configReload:  make(chan struct{}, 1),
		watching:      make(map[string]struct{}),
	}
}

func normalizeTiming(interval, blockedIdle, idleKillAfter time.Duration) (time.Duration, time.Duration, time.Duration) {
	if interval <= 0 {
		interval = DefaultPollInterval
	}
	if blockedIdle <= 0 {
		blockedIdle = DefaultBlockedIdle
	}
	if idleKillAfter < 0 {
		idleKillAfter = 0
	}
	return interval, blockedIdle, idleKillAfter
}

func (m *Monitor) Timing() (time.Duration, time.Duration, time.Duration) {
	m.configMu.RLock()
	defer m.configMu.RUnlock()
	return m.interval, m.blockedIdle, m.idleKillAfter
}

func (m *Monitor) SetTiming(interval, blockedIdle, idleKillAfter time.Duration) bool {
	interval, blockedIdle, idleKillAfter = normalizeTiming(interval, blockedIdle, idleKillAfter)

	m.configMu.Lock()
	changed := m.interval != interval || m.blockedIdle != blockedIdle || m.idleKillAfter != idleKillAfter
	if changed {
		m.interval = interval
		m.blockedIdle = blockedIdle
		m.idleKillAfter = idleKillAfter
	}
	m.configMu.Unlock()

	if changed {
		select {
		case m.configReload <- struct{}{}:
		default:
		}
	}
	return changed
}

func runKey(ticketID, runID string) string {
	return ticketID + "/" + runID
}

// writeAndNotify persists as and, on success, fires OnStatusChange so
// callers can resync derived state (e.g. ticket frontmatter).
func (m *Monitor) writeAndNotify(as AgentStatus) error {
	if err := Write(m.root, as); err != nil {
		return err
	}
	if m.OnStatusChange != nil && !IsCronOwner(as.TicketID) {
		m.OnStatusChange(as.TicketID)
	}
	return nil
}

// TrackRun registers a (ticket, run) pair as having an active
// waitForSession goroutine. The monitor will not mark tracked runs
// as failed (that's the wait goroutine's job).
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

// Reconcile checks all non-terminal run files against session state.
// Sessions that are still alive are returned so the caller can spawn
// new wait goroutines for them. Sessions that are gone are marked
// failed.
func (m *Monitor) Reconcile() ([]AliveStatus, error) {
	statuses, err := m.listStatuses()
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
				if err := m.writeAndNotify(as); err != nil {
					log.Printf("monitor: failed to promote %s/%s to running: %v", as.TicketID, as.RunID, err)
				}
			}
			alive = append(alive, AliveStatus{as})
		} else {
			as.Status = StatusFailed
			as.Error = "session not found on watcher restart"
			if err := m.writeAndNotify(as); err != nil {
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
	interval, _, _ := m.Timing()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.configReload:
			nextInterval, _, _ := m.Timing()
			if nextInterval != interval {
				ticker.Reset(nextInterval)
				interval = nextInterval
			}
		case <-ticker.C:
			m.poll()
		}
	}
}

const staleAge = 24 * time.Hour

func (m *Monitor) poll() {
	_, blockedIdle, idleKillAfter := m.Timing()
	blockedAfterSeconds := int(blockedIdle / time.Second)
	idleKillAfterSeconds := int(idleKillAfter / time.Second)

	statuses, err := m.listStatuses()
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
				continue
			}
			// Terminal and older than staleAge. Drop the run files
			// individually so a ticket that's re-run forever doesn't
			// accumulate history indefinitely — the all-or-nothing
			// RemoveTicket sweep below only fires when every run
			// qualifies at once.
			if err := os.Remove(runPath(m.root, as.TicketID, as.RunID)); err != nil && !os.IsNotExist(err) {
				log.Printf("monitor: failed to prune stale run %s/%s: %v", as.TicketID, as.RunID, err)
			}
			if err := os.Remove(LogPath(m.root, as.TicketID, as.RunID)); err != nil && !os.IsNotExist(err) {
				log.Printf("monitor: failed to prune stale log %s/%s: %v", as.TicketID, as.RunID, err)
			}
			continue
		}
		ticketsWithLiveRuns[as.TicketID] = true

		if m.check(as.Session) {
			idle := m.idleCheck(as.Session)
			if idleKillAfterSeconds > 0 && idle >= idleKillAfterSeconds {
				if m.kill == nil {
					log.Printf("monitor: %s/%s exceeded idle kill threshold but no killer is configured", as.TicketID, as.RunID)
					continue
				}
				if err := m.kill(as.Session); err != nil {
					log.Printf("monitor: failed to kill idle session %s for %s/%s: %v", as.Session, as.TicketID, as.RunID, err)
					continue
				}
				as.Status = StatusFailed
				as.Error = fmt.Sprintf("session killed after %ds idle", idle)
				if err := m.writeAndNotify(as); err != nil {
					log.Printf("monitor: failed to mark %s/%s failed after idle kill: %v", as.TicketID, as.RunID, err)
				} else {
					log.Printf("monitor: %s/%s killed after %ds idle", as.TicketID, as.RunID, idle)
				}
				continue
			}

			switch as.Status {
			case StatusSpawned:
				as.Status = StatusRunning
				if err := m.writeAndNotify(as); err != nil {
					log.Printf("monitor: failed to promote %s/%s to running: %v", as.TicketID, as.RunID, err)
				}
			case StatusRunning:
				if idle >= blockedAfterSeconds {
					as.Status = StatusBlocked
					as.Error = fmt.Sprintf("pane idle for %ds, likely waiting for input", idle)
					if err := m.writeAndNotify(as); err != nil {
						log.Printf("monitor: failed to mark %s/%s blocked: %v", as.TicketID, as.RunID, err)
					} else {
						log.Printf("monitor: %s/%s marked blocked (idle %ds)", as.TicketID, as.RunID, idle)
					}
				}
			case StatusBlocked:
				if idle < blockedAfterSeconds {
					as.Status = StatusRunning
					as.Error = ""
					if err := m.writeAndNotify(as); err != nil {
						log.Printf("monitor: failed to unblock %s/%s: %v", as.TicketID, as.RunID, err)
					} else {
						log.Printf("monitor: %s/%s resumed (no longer idle)", as.TicketID, as.RunID)
					}
				}
			}
		} else if !m.isTracked(as.TicketID, as.RunID) {
			as.Status = StatusFailed
			as.Error = "session disappeared"
			if err := m.writeAndNotify(as); err != nil {
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
		if e.Name() == cronNamespace {
			// Cron owner dirs are owned by user config; only
			// `tickets doctor --fix` prunes orphans. Skipping also
			// avoids RemoveTicket(".cron") wiping every cron owner
			// at once, since no live-run key equals bare ".cron".
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

func (m *Monitor) listStatuses() ([]AgentStatus, error) {
	runs, err := ListAll(m.root)
	if err != nil {
		return nil, err
	}
	cronRuns, err := ListAllCronRuns(m.root)
	if err != nil {
		return nil, err
	}
	return append(runs, cronRuns...), nil
}
