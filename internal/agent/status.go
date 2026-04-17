// Package agent provides persistent, filesystem-backed status tracking
// for AI agents spawned by `tickets watch`. Each ticket gets its own
// directory under .tickets/.agents/<ticket-id>/, holding one <run>.yml
// per agent run and a runs/ subdirectory with the matching .log file.
// Run ids are <NNN>-<stage> where NNN is a per-ticket monotonic
// sequence — so a ticket that revisits "execute" gets a fresh run
// with a higher sequence number.
package agent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/stepandel/tickets-md/internal/config"

	"gopkg.in/yaml.v3"
)

// Status represents where an agent is in its lifecycle.
type Status string

const (
	StatusSpawned Status = "spawned" // session creation requested
	StatusRunning Status = "running" // session confirmed alive
	StatusBlocked Status = "blocked" // agent idle, likely waiting for user input
	StatusDone    Status = "done"    // agent exited successfully (exit code 0)
	StatusFailed  Status = "failed"  // agent exited with error (non-zero exit)
	StatusErrored Status = "errored" // couldn't create the session at all
)

// IsTerminal reports whether the status is a final state with no
// outbound transitions.
func (s Status) IsTerminal() bool {
	return s == StatusDone || s == StatusFailed || s == StatusErrored
}

// validTransitions defines the state machine. Terminal states have no
// outbound edges.
var validTransitions = map[Status][]Status{
	// spawned -> done covers fast exits that finish before the monitor's
	// next poll promotes the run to running.
	StatusSpawned: {StatusRunning, StatusDone, StatusErrored, StatusFailed},
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
	TicketID    string    `yaml:"ticket_id"`
	RunID       string    `yaml:"run_id"`  // e.g. "002-execute"
	Seq         int       `yaml:"seq"`     // monotonic per-ticket
	Attempt     int       `yaml:"attempt"` // per-stage attempt counter
	Stage       string    `yaml:"stage"`
	Agent       string    `yaml:"agent"`
	Session     string    `yaml:"session"`
	Status      Status    `yaml:"status"`
	SpawnedAt   time.Time `yaml:"spawned_at"`
	UpdatedAt   time.Time `yaml:"updated_at"`
	ExitCode    *int      `yaml:"exit_code,omitempty"`
	Error       string    `yaml:"error,omitempty"`
	LogFile     string    `yaml:"log_file"`
	Worktree    string    `yaml:"worktree,omitempty"`
	SessionUUID string    `yaml:"session_uuid,omitempty"`
	PlanFile    string    `yaml:"plan_file,omitempty"`
	ResumedFrom string    `yaml:"resumed_from,omitempty"`
}

const agentsDir = ".agents"
const cronNamespace = ".cron"

// Dir returns the absolute path to .tickets/.agents/.
func Dir(root string) string {
	return filepath.Join(root, config.ConfigDir, agentsDir)
}

// TicketDir returns the absolute path to .tickets/.agents/<ticket-id>/.
func TicketDir(root, ticketID string) string {
	return filepath.Join(Dir(root), ticketID)
}

// RunsDir returns the absolute path to .tickets/.agents/<ticket-id>/runs/,
// which holds the .log artifacts for every run.
func RunsDir(root, ticketID string) string {
	return filepath.Join(TicketDir(root, ticketID), "runs")
}

func runPath(root, ticketID, runID string) string {
	return filepath.Join(TicketDir(root, ticketID), runID+".yml")
}

// LogPath returns the canonical log file path for a run.
func LogPath(root, ticketID, runID string) string {
	return filepath.Join(RunsDir(root, ticketID), runID+".log")
}

// runIDRegex matches "<seq>-<stage>" with a 3+ digit zero-padded seq.
var runIDRegex = regexp.MustCompile(`^(\d{3,})-(.+)$`)

// FormatRunID renders a run id from its components.
func FormatRunID(seq int, stage string) string {
	return fmt.Sprintf("%03d-%s", seq, stage)
}

// ParseRunID extracts the seq and stage from a run id string.
func ParseRunID(runID string) (seq int, stage string, ok bool) {
	m := runIDRegex.FindStringSubmatch(runID)
	if m == nil {
		return 0, "", false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, "", false
	}
	return n, m[2], true
}

// NextRun computes the next run id for a ticket entering a given stage.
// It returns the formatted run id, its sequence number, and the
// per-stage attempt counter (1 for the first time the ticket visits
// this stage, 2 for the second, …). Callers must follow up with Write
// using StatusSpawned to actually create the run file.
func NextRun(root, ticketID, stage string) (runID string, seq, attempt int, err error) {
	runs, err := History(root, ticketID)
	if err != nil {
		return "", 0, 0, err
	}
	seq = 1
	attempt = 1
	for _, r := range runs {
		if r.Seq >= seq {
			seq = r.Seq + 1
		}
		if r.Stage == stage {
			attempt++
		}
	}
	return FormatRunID(seq, stage), seq, attempt, nil
}

// Write persists an AgentStatus to disk under
// .tickets/.agents/<ticket-id>/<run-id>.yml. For non-spawn writes,
// the transition is validated against the current state of that run.
// Spawn writes use O_EXCL to surface races where two callers picked
// the same run id; callers should retry with a fresh NextRun.
// Writes are atomic (temp file + rename) to prevent partial reads.
func Write(root string, as AgentStatus) error {
	if as.RunID == "" {
		return fmt.Errorf("agent.Write: RunID is required")
	}

	isNew := true
	if as.Status != StatusSpawned {
		existing, err := ReadRun(root, as.TicketID, as.RunID)
		if err == nil {
			isNew = false
			if err := Transition(existing.Status, as.Status); err != nil {
				return fmt.Errorf("status transition for %s/%s: %w", as.TicketID, as.RunID, err)
			}
		}
	}

	as.UpdatedAt = time.Now().UTC().Truncate(time.Second)

	dir := TicketDir(root, as.TicketID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	data, err := yaml.Marshal(as)
	if err != nil {
		return err
	}

	target := runPath(root, as.TicketID, as.RunID)

	// New files (spawned or first-write followups) use O_EXCL to
	// surface races where two callers picked the same run id.
	if isNew {
		f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return fmt.Errorf("creating run file %s: %w", target, err)
		}
		if _, werr := f.Write(data); werr != nil {
			f.Close()
			os.Remove(target)
			return werr
		}
		return f.Close()
	}

	tmp, err := writeRunTmp(target, data)
	if err != nil {
		return err
	}
	return os.Rename(tmp, target)
}

// writeRunTmp writes data to a uniquely-named sibling of target and
// returns the temp path. Concurrent writers to the same target get
// distinct tmp files (so they never truncate each other's buffer),
// and the resulting filename still ends in ".yml.tmp" so
// doctor's orphan-tmp scan continues to match it.
func writeRunTmp(target string, data []byte) (string, error) {
	dir := filepath.Dir(target)
	prefix := strings.TrimSuffix(filepath.Base(target), ".yml")
	f, err := os.CreateTemp(dir, prefix+".*.yml.tmp")
	if err != nil {
		return "", err
	}
	tmp := f.Name()
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return "", err
	}
	return tmp, nil
}

// ReadRun loads a specific run record.
func ReadRun(root, ticketID, runID string) (AgentStatus, error) {
	data, err := os.ReadFile(runPath(root, ticketID, runID))
	if err != nil {
		return AgentStatus{}, err
	}
	var as AgentStatus
	if err := yaml.Unmarshal(data, &as); err != nil {
		return AgentStatus{}, err
	}
	return as, nil
}

// Latest returns the highest-seq run for a ticket. Returns os.ErrNotExist
// if the ticket has no recorded runs.
func Latest(root, ticketID string) (AgentStatus, error) {
	runs, err := History(root, ticketID)
	if err != nil {
		return AgentStatus{}, err
	}
	if len(runs) == 0 {
		return AgentStatus{}, os.ErrNotExist
	}
	return runs[len(runs)-1], nil
}

// History returns every run for a ticket, sorted by sequence number
// ascending. An empty result (no directory, no runs) is not an error.
func History(root, ticketID string) ([]AgentStatus, error) {
	dir := TicketDir(root, ticketID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var runs []AgentStatus
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".yml") || strings.HasSuffix(name, ".tmp") {
			continue
		}
		runID := strings.TrimSuffix(name, ".yml")
		if _, _, ok := ParseRunID(runID); !ok {
			continue
		}
		as, err := ReadRun(root, ticketID, runID)
		if err != nil {
			continue
		}
		runs = append(runs, as)
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].Seq < runs[j].Seq })
	return runs, nil
}

// List returns the latest run for every ticket that has any recorded
// run, sorted by spawn time. This is the default view for board and
// `tickets agents`.
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
		if !e.IsDir() || e.Name() == cronNamespace {
			continue
		}
		as, err := Latest(root, e.Name())
		if err != nil {
			continue
		}
		statuses = append(statuses, as)
	}
	sort.Slice(statuses, func(i, j int) bool {
		return statuses[i].SpawnedAt.Before(statuses[j].SpawnedAt)
	})
	return statuses, nil
}

// ListAll returns every run across every ticket, sorted by spawn time.
// Used by the monitor for reconciliation.
func ListAll(root string) ([]AgentStatus, error) {
	dir := Dir(root)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var all []AgentStatus
	for _, e := range entries {
		if !e.IsDir() || e.Name() == cronNamespace {
			continue
		}
		runs, err := History(root, e.Name())
		if err != nil {
			continue
		}
		all = append(all, runs...)
	}
	sort.Slice(all, func(i, j int) bool {
		return all[i].SpawnedAt.Before(all[j].SpawnedAt)
	})
	return all, nil
}

// SetPlanFile overwrites the plan_file field on an existing run
// without running a transition check, so it can be used to backfill
// terminal runs whose capture missed the plan path at session end.
func SetPlanFile(root, ticketID, runID, planFile string) error {
	as, err := ReadRun(root, ticketID, runID)
	if err != nil {
		return err
	}
	as.PlanFile = planFile
	as.UpdatedAt = time.Now().UTC().Truncate(time.Second)

	data, err := yaml.Marshal(as)
	if err != nil {
		return err
	}
	target := runPath(root, ticketID, runID)
	tmp, err := writeRunTmp(target, data)
	if err != nil {
		return err
	}
	return os.Rename(tmp, target)
}

// RemoveTicket deletes the entire .agents/<ticket-id>/ directory.
func RemoveTicket(root, ticketID string) error {
	return os.RemoveAll(TicketDir(root, ticketID))
}
