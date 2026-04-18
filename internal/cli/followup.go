package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/stepandel/tickets-md/internal/agent"
)

const (
	defaultMaxLogLines  = 200
	defaultMaxDiffLines = 8000
)

func newAgentsFollowupCmd() *cobra.Command {
	var runID, message string
	cmd := &cobra.Command{
		Use:   "followup <ticket-id>",
		Short: "Start a followup agent session with prior context",
		Long: `Spawn a fresh agent session enriched with context from a
previous run. The agent receives the git diff, PTY log, and ticket
body from the prior run so it can continue the work.

This is agent-agnostic — it works with claude, aider, codex, or any
CLI agent that was used in the original run.

By default the latest terminal (done/failed) run is used as context.
Use --run to specify a different run.

Examples:
  tickets agents followup TIC-001 --message "also add tests"
  tickets agents followup TIC-001 --run 002-execute
  tickets agents followup TIC-001  # interactive, context only`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStoreAuto(cmd)
			if err != nil {
				return err
			}
			ticketID := args[0]

			// Look up the target run.
			var sourceRun agent.AgentStatus
			if runID != "" {
				sourceRun, err = agent.ReadRun(s.Root, ticketID, runID)
				if err != nil {
					return fmt.Errorf("no run %s for %s: %w", runID, ticketID, err)
				}
			} else {
				sourceRun, err = latestTerminalRun(s.Root, ticketID)
				if err != nil {
					return err
				}
			}

			// Validate: run must be terminal.
			if !sourceRun.Status.IsTerminal() {
				return fmt.Errorf("run %s is still %s — wait for it to finish or kill it", sourceRun.RunID, sourceRun.Status)
			}

			// Find the ticket to get its path and metadata.
			t, err := s.Get(ticketID)
			if err != nil {
				return fmt.Errorf("ticket %s not found: %w", ticketID, err)
			}

			// Warn if worktree is gone.
			var worktreeGone bool
			if sourceRun.Worktree != "" {
				if _, err := os.Stat(sourceRun.Worktree); os.IsNotExist(err) {
					fmt.Fprintf(os.Stderr, "warning: worktree %s no longer exists, skipping diff\n", sourceRun.Worktree)
					worktreeGone = true
				}
			}

			// Gather context from the previous run.
			ctx, err := agent.GatherContext(s.Root, sourceRun, t.Path, defaultMaxLogLines, defaultMaxDiffLines)
			if err != nil {
				return fmt.Errorf("gathering context: %w", err)
			}

			// Determine cwd: worktree if it exists, otherwise repo root.
			cwd := sourceRun.Worktree
			if cwd == "" || worktreeGone {
				cwd = s.Root
			}

			// Compute the followup run ID before spawning.
			followupRunID, seq, attempt, err := agent.NextRun(s.Root, ticketID, "followup")
			if err != nil {
				return fmt.Errorf("computing followup run id: %w", err)
			}
			sessionName := fmt.Sprintf("%s-%d", ticketID, seq)

			// Ensure runs directory exists.
			if err := os.MkdirAll(agent.RunsDir(s.Root, ticketID), 0o755); err != nil {
				return fmt.Errorf("creating runs dir: %w", err)
			}

			// Write the enriched context to a file to avoid ARG_MAX
			// limits on large diffs. The agent reads this file instead
			// of receiving the full context as a CLI argument.
			prompt := composeFollowupPrompt(ticketID, t.Title, t.Path, cwd, ctx, message)
			contextPath := filepath.Join(agent.RunsDir(s.Root, ticketID), followupRunID+"-context.md")
			if err := os.WriteFile(contextPath, []byte(prompt), 0o644); err != nil {
				return fmt.Errorf("writing followup context: %w", err)
			}

			shortPrompt := fmt.Sprintf("Read the followup context at %s and follow the instructions in it.", contextPath)

			// Build argv: same agent command, no --print (interactive).
			// Let the agent's integration inject any startup flags
			// (e.g. a fresh session id) for run tracking.
			var extraArgs []string
			var sessionUUID string
			if integ, ok := agent.Lookup(sourceRun.Agent); ok {
				newArgs, id, err := integ.PrepareArgs(nil)
				if err == nil {
					extraArgs = newArgs
					sessionUUID = id
				}
			}

			argv := append([]string{sourceRun.Agent}, extraArgs...)
			argv = append(argv, shortPrompt)

			fmt.Fprintf(os.Stderr, "Following up on %s/%s (agent: %s)\n", ticketID, sourceRun.RunID, sourceRun.Agent)
			fmt.Fprintf(os.Stderr, "Context written to %s\n", contextPath)
			if cwd != s.Root {
				fmt.Fprintf(os.Stderr, "Working directory: %s\n", cwd)
			}

			// Run interactively with stdin/stdout/stderr connected.
			c := exec.Command(argv[0], argv[1:]...)
			c.Dir = cwd
			c.Stdin = os.Stdin
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr

			startTime := time.Now().UTC().Truncate(time.Second)
			runErr := c.Run()

			// Determine exit status.
			exitCode := 0
			finalStatus := agent.StatusDone
			var statusErr string
			if runErr != nil {
				if exitErr, ok := runErr.(*exec.ExitError); ok {
					exitCode = exitErr.ExitCode()
				} else {
					exitCode = 1
				}
				finalStatus = agent.StatusFailed
				statusErr = fmt.Sprintf("agent exited with code %d", exitCode)
			}

			// Record the followup run ONLY after exit to avoid the
			// monitor's poll() marking it as failed. New file creation
			// uses O_EXCL to catch run-id races.
			as := agent.AgentStatus{
				TicketID:    ticketID,
				RunID:       followupRunID,
				Seq:         seq,
				Attempt:     attempt,
				Stage:       "followup",
				Agent:       sourceRun.Agent,
				Session:     sessionName,
				Status:      finalStatus,
				SpawnedAt:   startTime,
				ExitCode:    &exitCode,
				Error:       statusErr,
				Worktree:    sourceRun.Worktree,
				SessionUUID: sessionUUID,
				ResumedFrom: sourceRun.RunID,
			}

			if err := agent.Write(s.Root, as); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to record followup run: %v\n", err)
			}
			syncAgentFrontmatter(s.Root, ticketID)

			if runErr != nil {
				return fmt.Errorf("agent exited with code %d", exitCode)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&runID, "run", "", "source run id to follow up on (default: latest terminal run)")
	cmd.Flags().StringVarP(&message, "message", "m", "", "followup instruction for the agent")
	return cmd
}

// latestTerminalRun returns the most recent terminal (done/failed/errored)
// run for a ticket.
func latestTerminalRun(root, ticketID string) (agent.AgentStatus, error) {
	runs, err := agent.History(root, ticketID)
	if err != nil {
		return agent.AgentStatus{}, err
	}
	// Walk backwards to find the latest terminal run.
	for i := len(runs) - 1; i >= 0; i-- {
		if runs[i].Status.IsTerminal() {
			return runs[i], nil
		}
	}
	return agent.AgentStatus{}, fmt.Errorf("no terminal runs found for %s", ticketID)
}

// composeFollowupPrompt builds the enriched prompt for the followup agent.
func composeFollowupPrompt(ticketID, title, ticketPath, worktree string, ctx agent.RunContext, message string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are following up on previous work for ticket %s: %s.\n", ticketID, title)

	if ctx.Diff != "" {
		b.WriteString("\n## Changes made by previous run\n```diff\n")
		b.WriteString(ctx.Diff)
		b.WriteString("\n```\n")
	}

	if ctx.Log != "" {
		b.WriteString("\n## Previous agent output\n")
		b.WriteString(ctx.Log)
		b.WriteString("\n")
	}

	if message != "" {
		b.WriteString("\n## Follow-up\n")
		b.WriteString(message)
		b.WriteString("\n")
	}

	fmt.Fprintf(&b, "\nThe full ticket is at %s.", ticketPath)
	if worktree != "" {
		fmt.Fprintf(&b, " The worktree is at %s.", worktree)
	}
	b.WriteString("\n")

	return b.String()
}
