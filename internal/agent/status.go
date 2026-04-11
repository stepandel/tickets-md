// Package agent provides persistent, filesystem-backed status tracking
// for AI agents spawned by `tickets watch`. Each ticket with an active
// (or recently completed) agent gets a YAML status file under
// .tickets/.agents/{ticket-id}.yml.
package agent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"tickets-md/internal/config"

	"gopkg.in/yaml.v3"
)

// Status represents where an agent is in its lifecycle.
type Status string

const (
	StatusSpawned Status = "spawned" // tmux session creation requested
	StatusRunning Status = "running" // tmux session confirmed alive
	StatusBlocked Status = "blocked" // agent idle, likely waiting for user input
	StatusDone    Status = "done"    // agent exited successfully (exit code 0)
	StatusFailed  Status = "failed"  // agent exited with error (non-zero exit)
	StatusErrored Status = "errored" // couldn't create the tmux session at all
)

// IsTerminal reports whether the status is a final state with no
// outbound transitions.
func (s Status) IsTerminal() bool {
	return s == StatusDone || s == StatusFailed || s == StatusErrored
}

// validTransitions defines the state machine. Terminal states have no
// outbound edges.
var validTransitions = map[Status][]Status{
	StatusSpawned: {StatusRunning, StatusErrored, StatusFailed},
	StatusRunning: {StatusDone, StatusFailed, StatusBlocked},
	StatusBlocked: {StatusRunning, StatusDone, StatusFailed},
}

// Transition validates that moving from → to is a legal state change.
func Transition(from, to Status) error {
	allowed, ok := validTransitions[from]
	if !ok {
		return fmt.Errorf("no transitions from terminal state %q", from)
	}
	for _, a := range allowed {
		if a == to {
			return nil
		}
	}
	return fmt.Errorf("invalid transition %q -> %q", from, to)
}

// AgentStatus is the persistent record of a single agent run, stored
// as a YAML file on disk.
type AgentStatus struct {
	TicketID  string    `yaml:"ticket_id"`
	Stage     string    `yaml:"stage"`
	Agent     string    `yaml:"agent"`
	Session   string    `yaml:"session"`
	Status    Status    `yaml:"status"`
	SpawnedAt time.Time `yaml:"spawned_at"`
	UpdatedAt time.Time `yaml:"updated_at"`
	ExitCode  *int      `yaml:"exit_code,omitempty"`
	Error     string    `yaml:"error,omitempty"`
	LogFile   string    `yaml:"log_file"`
	Worktree  string    `yaml:"worktree,omitempty"`
}

const agentsDir = ".agents"

// Dir returns the absolute path to .tickets/.agents/.
func Dir(root string) string {
	return filepath.Join(root, config.ConfigDir, agentsDir)
}

func statusPath(root, ticketID string) string {
	return filepath.Join(Dir(root), ticketID+".yml")
}

// Write persists an AgentStatus to disk. If a status file already
// exists for the ticket, the transition is validated against the
// current state. Writing "spawned" always succeeds (it starts a
// fresh run, overwriting any previous terminal state). Writes are
// atomic (temp file + rename) to prevent partial reads.
func Write(root string, as AgentStatus) error {
	if as.Status != StatusSpawned {
		existing, err := Read(root, as.TicketID)
		if err == nil {
			if err := Transition(existing.Status, as.Status); err != nil {
				return fmt.Errorf("status transition for %s: %w", as.TicketID, err)
			}
		}
	}

	as.UpdatedAt = time.Now().UTC().Truncate(time.Second)

	dir := Dir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	data, err := yaml.Marshal(as)
	if err != nil {
		return err
	}

	target := statusPath(root, as.TicketID)
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, target)
}

// Read loads the status file for a single ticket.
func Read(root, ticketID string) (AgentStatus, error) {
	data, err := os.ReadFile(statusPath(root, ticketID))
	if err != nil {
		return AgentStatus{}, err
	}
	var as AgentStatus
	if err := yaml.Unmarshal(data, &as); err != nil {
		return AgentStatus{}, err
	}
	return as, nil
}

// List returns all agent statuses, sorted by spawn time.
func List(root string) ([]AgentStatus, error) {
	dir := Dir(root)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var statuses []AgentStatus
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yml") || strings.HasSuffix(e.Name(), ".tmp") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var as AgentStatus
		if err := yaml.Unmarshal(data, &as); err != nil {
			continue
		}
		statuses = append(statuses, as)
	}
	sort.Slice(statuses, func(i, j int) bool {
		return statuses[i].SpawnedAt.Before(statuses[j].SpawnedAt)
	})
	return statuses, nil
}

// Remove deletes the status file for a ticket.
func Remove(root, ticketID string) error {
	return os.Remove(statusPath(root, ticketID))
}
