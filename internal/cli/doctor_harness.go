package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/stepandel/tickets-md/internal/agent"
	"github.com/stepandel/tickets-md/internal/ticket"
	"github.com/stepandel/tickets-md/internal/worktree"
)

// HarnessIssueKind classifies a harness-level drift issue detected by
// HarnessDoctor. These complement the link-integrity issues handled by
// ticket.Store.Doctor.
type HarnessIssueKind string

const (
	// StaleRun: a non-terminal run YAML whose UpdatedAt is older than
	// the stale threshold. Almost always means the watcher crashed
	// before it could finalize the run.
	StaleRun HarnessIssueKind = "stale-run"
	// OrphanAgentDir: a directory under .tickets/.agents/<id>/ whose
	// ticket id cannot be located in any stage.
	OrphanAgentDir HarnessIssueKind = "orphan-agent-dir"
	// OrphanTmp: a stray <run>.yml.tmp left behind by an interrupted
	// atomic rename.
	OrphanTmp HarnessIssueKind = "orphan-tmp"
	// OrphanWorktree: a directory under .worktrees/<id>/ whose ticket
	// id cannot be located in any stage.
	OrphanWorktree HarnessIssueKind = "orphan-worktree"
	// FrontmatterDrift: a ticket's agent_* frontmatter fields disagree
	// with the latest run YAML.
	FrontmatterDrift HarnessIssueKind = "frontmatter-drift"
)

// HarnessIssue describes a single drift finding.
type HarnessIssue struct {
	Kind    HarnessIssueKind
	Target  string
	Message string
	Fixed   bool
}

// String renders a HarnessIssue for CLI output.
func (h HarnessIssue) String() string {
	action := "found"
	if h.Fixed {
		action = "fixed"
	}
	return fmt.Sprintf("[doctor] %s: %s — %s (%s)", h.Kind, h.Target, h.Message, action)
}

// DefaultStaleAfter is the wall-clock age at which a non-terminal run
// is considered abandoned. It is deliberately generous because doctor
// runs offline — it cannot distinguish a legitimately long-running
// agent from one whose watcher died. Configurable via --stale-after.
const DefaultStaleAfter = 24 * time.Hour

// AutoHeal runs the non-destructive subset of the harness doctor —
// frontmatter drift and orphan .yml.tmp files — and returns the
// issues it found (all Fixed). It is safe to call on watcher startup:
// neither check can destroy data or kill a live agent. Destructive
// fixes (stale-run failures, orphan agent dirs, orphan worktrees)
// stay behind an explicit `tickets doctor` invocation.
func AutoHeal(s *ticket.Store) ([]HarnessIssue, error) {
	tickets, err := s.ListAll()
	if err != nil {
		return nil, err
	}
	knownTickets := make(map[string]ticket.Ticket)
	for _, ts := range tickets {
		for _, t := range ts {
			knownTickets[t.ID] = t
		}
	}
	var issues []HarnessIssue
	issues = append(issues, checkFrontmatterDrift(s, true, knownTickets)...)
	issues = append(issues, checkOrphanTmpFiles(s.Root, true)...)
	return issues, nil
}

// HarnessDoctor walks the whole store for cross-cutting drift and
// optionally repairs it. Returns the full list of issues; each issue's
// Fixed field records whether the repair succeeded.
//
// Categories:
//   - stale non-terminal runs → flipped to failed
//   - orphan .tickets/.agents/<id>/ dirs → removed
//   - orphan <run>.yml.tmp files → removed
//   - orphan .worktrees/<id>/ dirs → removed
//   - frontmatter drift → frontmatter rewritten from the latest run
func HarnessDoctor(s *ticket.Store, fix bool, staleAfter time.Duration) ([]HarnessIssue, error) {
	if staleAfter <= 0 {
		staleAfter = DefaultStaleAfter
	}
	root := s.Root

	// Resolve every live ticket id once; used by several checks below.
	tickets, err := s.ListAll()
	if err != nil {
		return nil, err
	}
	knownTickets := make(map[string]ticket.Ticket)
	for _, ts := range tickets {
		for _, t := range ts {
			knownTickets[t.ID] = t
		}
	}
	knownCronAgents := make(map[string]struct{}, len(s.Config.CronAgents))
	for _, ca := range s.Config.CronAgents {
		knownCronAgents[ca.Name] = struct{}{}
	}

	var issues []HarnessIssue

	issues = append(issues, checkStaleRuns(root, fix, staleAfter)...)
	issues = append(issues, checkOrphanAgentDirs(root, fix, knownTickets, knownCronAgents)...)
	issues = append(issues, checkOrphanTmpFiles(root, fix)...)
	issues = append(issues, checkOrphanWorktrees(root, fix, knownTickets)...)
	issues = append(issues, checkFrontmatterDrift(s, fix, knownTickets)...)

	return issues, nil
}

// checkStaleRuns finds non-terminal runs older than staleAfter and, in
// fix mode, marks them failed via agent.Write (which validates the
// transition).
func checkStaleRuns(root string, fix bool, staleAfter time.Duration) []HarnessIssue {
	runs, err := agent.ListAll(root)
	if err != nil {
		return nil
	}
	cronRuns, err := agent.ListAllCronRuns(root)
	if err == nil {
		runs = append(runs, cronRuns...)
	}
	var issues []HarnessIssue
	cutoff := time.Now().Add(-staleAfter)
	for _, as := range runs {
		if as.Status.IsTerminal() {
			continue
		}
		if as.UpdatedAt.After(cutoff) {
			continue
		}
		iss := HarnessIssue{
			Kind:    StaleRun,
			Target:  as.TicketID + "/" + as.RunID,
			Message: fmt.Sprintf("status=%s updated_at=%s (stale)", as.Status, as.UpdatedAt.Format(time.RFC3339)),
		}
		if fix {
			as.Status = agent.StatusFailed
			as.Error = "stale: watcher did not finalize"
			if err := agent.Write(root, as); err == nil {
				iss.Fixed = true
			} else {
				iss.Message = fmt.Sprintf("%s — fix failed: %v", iss.Message, err)
			}
		}
		issues = append(issues, iss)
	}
	return issues
}

// checkOrphanAgentDirs finds subdirectories of .tickets/.agents/ whose
// ticket id no longer exists anywhere in the store.
func checkOrphanAgentDirs(root string, fix bool, known map[string]ticket.Ticket, knownCronAgents map[string]struct{}) []HarnessIssue {
	dir := agent.Dir(root)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var issues []HarnessIssue
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		id := e.Name()
		if id == ".cron" {
			cronEntries, err := os.ReadDir(filepath.Join(dir, id))
			if err != nil {
				continue
			}
			for _, ce := range cronEntries {
				if !ce.IsDir() {
					continue
				}
				name := ce.Name()
				if _, ok := knownCronAgents[name]; ok {
					continue
				}
				iss := HarnessIssue{
					Kind:    OrphanAgentDir,
					Target:  filepath.Join(id, name),
					Message: "cron agent dir exists but no cron agent is configured",
				}
				if fix {
					if err := agent.RemoveCron(root, name); err == nil {
						iss.Fixed = true
					} else {
						iss.Message = fmt.Sprintf("%s — fix failed: %v", iss.Message, err)
					}
				}
				issues = append(issues, iss)
			}
			continue
		}
		if _, ok := known[id]; ok {
			continue
		}
		iss := HarnessIssue{
			Kind:    OrphanAgentDir,
			Target:  id,
			Message: "agent dir exists but no ticket found",
		}
		if fix {
			if err := agent.RemoveTicket(root, id); err == nil {
				iss.Fixed = true
			} else {
				iss.Message = fmt.Sprintf("%s — fix failed: %v", iss.Message, err)
			}
		}
		issues = append(issues, iss)
	}
	return issues
}

// checkOrphanTmpFiles finds <run>.yml.tmp files left behind by an
// interrupted atomic rename.
func checkOrphanTmpFiles(root string, fix bool) []HarnessIssue {
	dir := agent.Dir(root)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var issues []HarnessIssue
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if e.Name() == ".cron" {
			cronEntries, err := os.ReadDir(filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}
			for _, ce := range cronEntries {
				if !ce.IsDir() {
					continue
				}
				issues = append(issues, scanOrphanTmpDir(filepath.Join(dir, e.Name(), ce.Name()), filepath.Join(e.Name(), ce.Name()), fix)...)
			}
			continue
		}
		issues = append(issues, scanOrphanTmpDir(filepath.Join(dir, e.Name()), e.Name(), fix)...)
	}
	return issues
}

func scanOrphanTmpDir(dir, targetPrefix string, fix bool) []HarnessIssue {
	runEntries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var issues []HarnessIssue
	for _, re := range runEntries {
		if re.IsDir() || !strings.HasSuffix(re.Name(), ".yml.tmp") {
			continue
		}
		path := filepath.Join(dir, re.Name())
		iss := HarnessIssue{
			Kind:    OrphanTmp,
			Target:  filepath.Join(targetPrefix, re.Name()),
			Message: "leftover temp file from interrupted rename",
		}
		if fix {
			if err := os.Remove(path); err == nil {
				iss.Fixed = true
			} else {
				iss.Message = fmt.Sprintf("%s — fix failed: %v", iss.Message, err)
			}
		}
		issues = append(issues, iss)
	}
	return issues
}

// checkOrphanWorktrees finds worktree directories whose ticket id no
// longer resolves to a ticket in the store.
func checkOrphanWorktrees(root string, fix bool, known map[string]ticket.Ticket) []HarnessIssue {
	infos, err := worktree.List(root)
	if err != nil {
		return nil
	}
	var issues []HarnessIssue
	for _, info := range infos {
		id := filepath.Base(info.Path)
		if _, ok := known[id]; ok {
			continue
		}
		iss := HarnessIssue{
			Kind:    OrphanWorktree,
			Target:  id,
			Message: fmt.Sprintf("worktree at %s has no ticket", info.Path),
		}
		if fix {
			if err := worktree.Remove(root, id); err == nil {
				iss.Fixed = true
			} else {
				iss.Message = fmt.Sprintf("%s — fix failed: %v", iss.Message, err)
			}
		}
		issues = append(issues, iss)
	}
	return issues
}

// checkFrontmatterDrift compares each ticket's agent_* frontmatter to
// the latest run YAML and, in fix mode, rewrites the frontmatter to
// match. The YAML is authoritative; frontmatter is a projection.
func checkFrontmatterDrift(s *ticket.Store, fix bool, known map[string]ticket.Ticket) []HarnessIssue {
	var issues []HarnessIssue
	for id, t := range known {
		wantStatus, wantRun, wantSession := desiredFrontmatter(s.Root, id)
		if t.AgentStatus == wantStatus && t.AgentRun == wantRun && t.AgentSession == wantSession {
			continue
		}
		iss := HarnessIssue{
			Kind:   FrontmatterDrift,
			Target: id,
			Message: fmt.Sprintf("frontmatter agent_status=%q run=%q session=%q, latest run says status=%q run=%q session=%q",
				t.AgentStatus, t.AgentRun, t.AgentSession, wantStatus, wantRun, wantSession),
		}
		if fix {
			t.AgentStatus = wantStatus
			t.AgentRun = wantRun
			t.AgentSession = wantSession
			if err := s.Save(t); err == nil {
				iss.Fixed = true
			} else {
				iss.Message = fmt.Sprintf("%s — fix failed: %v", iss.Message, err)
			}
		}
		issues = append(issues, iss)
	}
	return issues
}

// desiredFrontmatter returns the agent_* values the ticket should
// carry given its latest run YAML. The projection rule: status and
// run are copied verbatim; session is copied only while the run is
// non-terminal (a finished run has no live session to point at).
func desiredFrontmatter(root, ticketID string) (status, run, session string) {
	latest, err := agent.Latest(root, ticketID)
	if err != nil {
		return "", "", ""
	}
	status = string(latest.Status)
	run = latest.RunID
	if !latest.Status.IsTerminal() {
		session = latest.Session
	}
	return
}
