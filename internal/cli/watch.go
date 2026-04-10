package cli

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"

	"tickets-md/internal/agent"
	"tickets-md/internal/config"
	"tickets-md/internal/stage"
	"tickets-md/internal/ticket"
	"tickets-md/internal/worktree"
)

func newWatchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Watch for ticket movements and spawn configured agents",
		Long: `watch is a long-running process that monitors every stage
directory for arriving tickets. When a ticket lands in a stage that
has a .stage.yml with an agent configured, the agent is spawned in
a named tmux session. Attach to watch or interact:

  tmux attach -t <ticket-id>

Agent output is appended to the ticket file when the agent exits.
Requires tmux (brew install tmux).

Create a .stage.yml in any stage directory to configure an agent:

  # .tickets/execute/.stage.yml
  agent:
    command: claude
    args: ["--print"]
    prompt: |
      Read the ticket at {{path}} and implement what it describes.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			return runWatch(s)
		},
	}
	return cmd
}

func ensureTmux() error {
	if _, err := exec.LookPath("tmux"); err == nil {
		return nil
	}
	if !isTerminal(os.Stdin) {
		return fmt.Errorf("tmux is required for `tickets watch`: brew install tmux")
	}
	fmt.Println("tmux is required for `tickets watch` but wasn't found on your PATH.")
	fmt.Print("Install it now via Homebrew? [Y/n]: ")

	var answer string
	fmt.Scanln(&answer)
	answer = strings.TrimSpace(strings.ToLower(answer))
	if answer != "" && answer != "y" && answer != "yes" {
		return fmt.Errorf("tmux is required: brew install tmux")
	}

	fmt.Println("Running: brew install tmux")
	cmd := exec.Command("brew", "install", "tmux")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to install tmux: %w", err)
	}
	fmt.Println()

	// Verify it's now on PATH.
	if _, err := exec.LookPath("tmux"); err != nil {
		return fmt.Errorf("tmux was installed but not found on PATH — try restarting your shell")
	}
	return nil
}

func runWatch(s *ticket.Store) error {
	if err := ensureTmux(); err != nil {
		return err
	}

	w, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("creating watcher: %w", err)
	}
	defer w.Close()

	// Start the agent status monitor.
	mon := agent.NewMonitor(s.Root, agent.TmuxSessionExists, agent.TmuxPaneIdle)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Reconcile stale statuses from a previous watcher run.
	alive, err := mon.Reconcile()
	if err != nil {
		log.Printf("monitor: startup reconciliation failed: %v", err)
	}
	for _, as := range alive {
		t, terr := s.Get(as.TicketID)
		if terr != nil {
			log.Printf("monitor: cannot re-attach %s: %v", as.TicketID, terr)
			continue
		}
		log.Printf("%s: re-attaching to running agent (session %s)", as.TicketID, as.Session)
		mon.TrackSession(as.TicketID)
		go waitForTmuxSession(t, as.Agent, as.Session, as.LogFile, s.Root, mon)
	}

	go mon.Run(ctx)

	// Load stage configs and register directories.
	stageConfigs := make(map[string]stage.Config)
	for _, st := range s.Config.Stages {
		dir := filepath.Join(s.Root, config.ConfigDir, st)
		sc, err := stage.Load(dir)
		if err != nil {
			return fmt.Errorf("loading stage config for %s: %w", st, err)
		}
		stageConfigs[st] = sc
		if err := w.Add(dir); err != nil {
			return fmt.Errorf("watching %s: %w", dir, err)
		}

		status := "no agent"
		if sc.HasAgent() {
			status = fmt.Sprintf("agent: %s", sc.Agent.Command)
		}
		log.Printf("watching %s/ (%s)", st, status)
	}

	log.Println("ready — move tickets between stages to trigger agents (ctrl+c to stop)")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	seen := make(map[string]time.Time)

	for {
		select {
		case <-sigCh:
			log.Println("shutting down")
			cancel()
			return nil

		case event, ok := <-w.Events:
			if !ok {
				return nil
			}

			if event.Has(fsnotify.Rename) || event.Has(fsnotify.Remove) {
				handleRemove(s.Root, event.Name)
				continue
			}

			if !event.Has(fsnotify.Create) {
				continue
			}
			if t, ok := seen[event.Name]; ok && time.Since(t) < time.Second {
				continue
			}
			seen[event.Name] = time.Now()

			handleCreate(s, stageConfigs, event.Name, mon)

		case err, ok := <-w.Errors:
			if !ok {
				return nil
			}
			log.Printf("watcher error: %v", err)
		}
	}
}

func handleCreate(s *ticket.Store, stageConfigs map[string]stage.Config, path string, mon *agent.Monitor) {
	dir := filepath.Dir(path)
	stageName := filepath.Base(dir)

	base := filepath.Base(path)
	if !strings.HasSuffix(base, ".md") {
		return
	}

	sc, ok := stageConfigs[stageName]
	if !ok || !sc.HasAgent() {
		log.Printf("%s → %s (no agent configured)", strings.TrimSuffix(base, ".md"), stageName)
		return
	}

	t, err := ticket.LoadFile(path, stageName)
	if err != nil {
		log.Printf("%s: failed to parse ticket: %v", base, err)
		return
	}

	spawnAgentTmux(t, sc, s.Root, mon)
}

// handleRemove is called when a ticket file disappears from a stage
// directory (Rename or Remove event). If the ticket has an active tmux
// session, it is killed and the status is updated.
func handleRemove(root, path string) {
	base := filepath.Base(path)
	if !strings.HasSuffix(base, ".md") {
		return
	}
	ticketID := strings.TrimSuffix(base, ".md")

	// Only act if there's a live tmux session for this ticket.
	if exec.Command("tmux", "has-session", "-t", ticketID).Run() != nil {
		return
	}

	log.Printf("%s: ticket moved, killing agent session", ticketID)
	if err := exec.Command("tmux", "kill-session", "-t", ticketID).Run(); err != nil {
		log.Printf("%s: failed to kill tmux session: %v", ticketID, err)
	}

	// The waitForTmuxSession goroutine will detect the session is gone
	// and handle the status update. But the exit code file won't exist
	// (agent didn't exit normally), so set the status explicitly.
	if as, err := agent.Read(root, ticketID); err == nil && !as.Status.IsTerminal() {
		as.Status = agent.StatusFailed
		as.Error = "ticket moved, agent terminated"
		if err := agent.Write(root, as); err != nil {
			log.Printf("%s: failed to update status: %v", ticketID, err)
		}
	}
}

// buildAgentArgs returns the full argv (without the command itself)
// for the agent invocation. worktreePath is the absolute path to the
// worktree (empty string if worktrees are disabled for this stage).
func buildAgentArgs(t ticket.Ticket, ac *stage.AgentConfig, worktreePath string) []string {
	prompt := stage.RenderPrompt(ac.Prompt, stage.PromptVars{
		Path:     t.Path,
		ID:       t.ID,
		Title:    t.Title,
		Stage:    t.Stage,
		Body:     t.Body,
		Worktree: worktreePath,
	})
	argv := make([]string, 0, len(ac.Args)+1)
	argv = append(argv, ac.Args...)
	argv = append(argv, prompt)
	return argv
}

// --- tmux spawner ---

// shellQuote wraps s in POSIX single quotes, escaping any embedded
// single quotes. Safe for embedding in `sh -c '...'` strings.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func spawnAgentTmux(t ticket.Ticket, sc stage.Config, root string, mon *agent.Monitor) {
	ac := sc.Agent

	sessionName := t.ID
	logFile := filepath.Join(os.TempDir(), fmt.Sprintf("tickets-%s.log", t.ID))
	exitFile := filepath.Join(os.TempDir(), fmt.Sprintf("tickets-%s.exit", t.ID))

	// Check for existing session (e.g. ticket moved twice quickly).
	if exec.Command("tmux", "has-session", "-t", sessionName).Run() == nil {
		log.Printf("%s: tmux session already exists, skipping", t.ID)
		return
	}

	// Create a git worktree if configured.
	var wtPath string
	if ac.Worktree {
		var err error
		wtPath, err = worktree.Create(root, t.ID, ac.BaseBranch)
		if err != nil {
			log.Printf("%s: failed to create worktree: %v", t.ID, err)
			return
		}
		worktree.EnsureGitignored(root)
		log.Printf("%s: created worktree at %s (branch %s)", t.ID, wtPath, worktree.BranchPrefix+t.ID)
	}

	argv := buildAgentArgs(t, ac, wtPath)

	// Write "spawned" status before creating the tmux session.
	now := time.Now().UTC().Truncate(time.Second)
	as := agent.AgentStatus{
		TicketID:  t.ID,
		Stage:     t.Stage,
		Agent:     ac.Command,
		Session:   sessionName,
		Status:    agent.StatusSpawned,
		SpawnedAt: now,
		LogFile:   logFile,
	}
	if err := agent.Write(root, as); err != nil {
		log.Printf("%s: failed to write agent status: %v", t.ID, err)
	}

	// Build the agent command.
	parts := []string{shellQuote(ac.Command)}
	for _, a := range argv {
		parts = append(parts, shellQuote(a))
	}
	agentCmd := strings.Join(parts, " ")

	// The shell command sets up pipe-pane FIRST (to capture all output
	// to the log file), then runs the agent. The exit code is written
	// to a separate file so waitForTmuxSession can determine success
	// vs failure. The agent still gets a real TTY.
	shellCmd := fmt.Sprintf(
		"tmux pipe-pane %s; %s; echo $? > %s",
		shellQuote(fmt.Sprintf("cat >> %s", logFile)),
		agentCmd,
		shellQuote(exitFile),
	)

	// Start the tmux session. If worktree is active, set the starting
	// directory so the agent's cwd is the isolated checkout.
	tmuxArgs := []string{"new-session", "-d", "-s", sessionName}
	if wtPath != "" {
		tmuxArgs = append(tmuxArgs, "-c", wtPath)
	}
	tmuxArgs = append(tmuxArgs, "sh", "-c", shellCmd)

	err := exec.Command("tmux", tmuxArgs...).Run()
	if err != nil {
		log.Printf("%s → %s: failed to create tmux session: %v", t.ID, t.Stage, err)
		as.Status = agent.StatusErrored
		as.Error = err.Error()
		agent.Write(root, as) // best-effort
		return
	}

	wtInfo := ""
	if wtPath != "" {
		wtInfo = fmt.Sprintf(" [worktree: %s]", worktree.BranchPrefix+t.ID)
	}
	log.Printf("%s → %s: agent running in tmux (attach with: tmux attach -t %s)%s", t.ID, t.Stage, sessionName, wtInfo)

	mon.TrackSession(t.ID)
	go waitForTmuxSession(t, ac.Command, sessionName, logFile, root, mon)
}

// waitForTmuxSession polls until the tmux session ends, updates the
// agent status, then reads the log file and appends output to the
// ticket file.
func waitForTmuxSession(t ticket.Ticket, agentName, sessionName, logFile, root string, mon *agent.Monitor) {
	defer mon.UntrackSession(t.ID)

	for {
		time.Sleep(time.Second)
		if exec.Command("tmux", "has-session", "-t", sessionName).Run() != nil {
			break
		}
	}

	log.Printf("%s: agent %s finished (session %s closed)", t.ID, agentName, sessionName)

	// Determine exit status from the .exit file written by the shell wrapper.
	exitFile := filepath.Join(os.TempDir(), fmt.Sprintf("tickets-%s.exit", t.ID))
	finalStatus := agent.StatusDone
	var exitCode *int
	var statusErr string

	if exitData, err := os.ReadFile(exitFile); err == nil {
		if code, err := strconv.Atoi(strings.TrimSpace(string(exitData))); err == nil {
			exitCode = &code
			if code != 0 {
				finalStatus = agent.StatusFailed
				statusErr = fmt.Sprintf("agent exited with code %d", code)
			}
		}
	}

	// Update status file.
	if as, err := agent.Read(root, t.ID); err == nil {
		as.Status = finalStatus
		as.ExitCode = exitCode
		as.Error = statusErr
		if werr := agent.Write(root, as); werr != nil {
			log.Printf("%s: failed to update agent status: %v", t.ID, werr)
		}
	}

	data, err := os.ReadFile(logFile)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("%s: failed to read agent output: %v", t.ID, err)
		}
		return
	}
	// Keep the log file around for post-mortem debugging.

	output := strings.TrimSpace(string(data))
	if output == "" {
		return
	}
	if err := appendAgentOutput(t.Path, agentName, output); err != nil {
		log.Printf("%s: failed to append agent output: %v", t.ID, err)
	} else {
		log.Printf("%s: output appended to %s", t.ID, filepath.Base(t.Path))
	}
}

// --- shared ---

// ansiRegex matches ANSI escape sequences (colors, cursor movement,
// etc.) that pipe-pane captures from the raw terminal output.
var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\x1b\].*?\x07|\x1b\[.*?[HJK]`)

func stripAnsi(s string) string {
	return ansiRegex.ReplaceAllString(s, "")
}

func appendAgentOutput(path, agent, output string) error {
	output = stripAnsi(output)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	now := time.Now().Format("2006-01-02 15:04:05")
	_, err = fmt.Fprintf(f, "\n## Agent Output (%s, %s)\n\n%s\n", agent, now, output)
	return err
}
