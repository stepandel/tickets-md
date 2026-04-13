package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"

	"tickets-md/internal/agent"
	"tickets-md/internal/config"
	"tickets-md/internal/stage"
	"tickets-md/internal/terminal"
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
a PTY session. View agent output with:

  tickets agents log <ticket-id>

Each agent run is recorded under .tickets/.agents/<id>/<run>.yml
with a .log sibling under runs/.

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

func runWatch(s *ticket.Store) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("creating watcher: %w", err)
	}
	defer w.Close()

	runner := agent.NewPTYRunner()

	// Start the terminal WebSocket server for live PTY access.
	termSrv := terminal.New(runner)
	port, termErr := termSrv.Start()
	if termErr != nil {
		log.Printf("terminal server: %v (live terminal access disabled)", termErr)
	} else {
		log.Printf("terminal server listening on 127.0.0.1:%d", port)
		writeTerminalServerFile(s.Root, port)
		defer func() {
			termSrv.Shutdown(context.Background())
			removeTerminalServerFile(s.Root)
		}()
	}

	// Start the agent status monitor.
	mon := agent.NewMonitor(s.Root, runner.Alive, runner.IdleSeconds)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Reconcile stale statuses from a previous watcher run.
	alive, err := mon.Reconcile()
	if err != nil {
		log.Printf("monitor: startup reconciliation failed: %v", err)
	}

	// Backfill plan_file for any terminal runs whose session-end
	// capture missed it. Self-heals runs that finished under an
	// older binary or raced transcript flushes.
	backfillPlanFiles(s.Root)
	// PTY sessions don't survive watcher restart (child gets SIGHUP),
	// so alive is always empty here. Kept for structural correctness.
	for _, as := range alive {
		t, terr := s.Get(as.TicketID)
		if terr != nil {
			log.Printf("monitor: cannot re-attach %s: %v", as.TicketID, terr)
			continue
		}
		log.Printf("%s/%s: re-attaching to running agent (session %s)", as.TicketID, as.RunID, as.Session)
		mon.TrackRun(as.TicketID, as.RunID)
		go waitForSession(t, as.RunID, as.Agent, as.Session, s.Root, mon, runner)
	}

	go mon.Run(ctx)

	// Load stage configs and register directories. knownPaths tracks
	// every ticket file we've already observed at its current path.
	// It's what lets us distinguish a real cross-directory move (which
	// empties the source path) from an agent's atomic in-place rewrite
	// (rename(tmp, foo.md), which leaves the path alive at a new inode).
	stageConfigs := make(map[string]stage.Config)
	knownPaths := make(map[string]bool)
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

		entries, err := os.ReadDir(dir)
		if err != nil {
			return fmt.Errorf("scanning %s: %w", dir, err)
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			knownPaths[filepath.Join(dir, e.Name())] = true
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

	for {
		select {
		case <-sigCh:
			log.Println("shutting down")
			runner.Shutdown()
			cancel()
			return nil

		case event, ok := <-w.Events:
			if !ok {
				return nil
			}

			if event.Has(fsnotify.Rename) || event.Has(fsnotify.Remove) {
				// If the file still exists at this path, it was an
				// atomic in-place rewrite — the ticket didn't leave.
				if _, err := os.Stat(event.Name); err == nil {
					continue
				}
				delete(knownPaths, event.Name)
				handleRemove(s, event.Name, runner)
				continue
			}

			if !event.Has(fsnotify.Create) {
				continue
			}
			// Already tracked at this path → this Create is the
			// second half of an atomic rewrite (or a spurious
			// re-registration). The ticket was already here.
			if knownPaths[event.Name] {
				continue
			}
			knownPaths[event.Name] = true

			handleCreate(s, stageConfigs, event.Name, mon, runner)

		case err, ok := <-w.Errors:
			if !ok {
				return nil
			}
			log.Printf("watcher error: %v", err)
		}
	}
}

func handleCreate(s *ticket.Store, stageConfigs map[string]stage.Config, path string, mon *agent.Monitor, runner *agent.PTYRunner) {
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

	spawnAgent(t, sc, s.Root, mon, runner)
}

// handleRemove is called when a ticket file disappears from a stage
// directory (Rename or Remove event). If the ticket's latest run is
// still active, its session is killed and the run is marked failed.
func handleRemove(s *ticket.Store, path string, runner *agent.PTYRunner) {
	base := filepath.Base(path)
	if !strings.HasSuffix(base, ".md") {
		return
	}
	ticketID := strings.TrimSuffix(base, ".md")

	// If the ticket still exists in another stage, it was moved — not
	// deleted. The Rename event fires for the old path, but the file
	// already lives at the new location. Don't kill the agent.
	if _, err := s.Get(ticketID); err == nil {
		return
	}

	as, err := agent.Latest(s.Root, ticketID)
	if err != nil || as.Status.IsTerminal() {
		return
	}

	if !runner.Alive(as.Session) {
		return
	}

	log.Printf("%s/%s: ticket removed, killing agent session", ticketID, as.RunID)
	if err := runner.Kill(as.Session); err != nil {
		log.Printf("%s: failed to kill session: %v", ticketID, err)
	}

	// The waitForSession goroutine will detect the process exit and
	// handle the status update. But the agent didn't exit normally,
	// so set the status explicitly.
	if cur, err := agent.ReadRun(s.Root, ticketID, as.RunID); err == nil && !cur.Status.IsTerminal() {
		cur.Status = agent.StatusFailed
		cur.Error = "ticket removed, agent terminated"
		if err := agent.Write(s.Root, cur); err != nil {
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
		Links:    t.LinksText(),
	})
	argv := make([]string, 0, len(ac.Args)+1)
	argv = append(argv, ac.Args...)
	argv = append(argv, prompt)
	return argv
}

// --- agent spawner ---

func spawnAgent(t ticket.Ticket, sc stage.Config, root string, mon *agent.Monitor, runner *agent.PTYRunner) {
	ac := sc.Agent

	runID, seq, attempt, err := agent.NextRun(root, t.ID, t.Stage)
	if err != nil {
		log.Printf("%s: failed to compute next run id: %v", t.ID, err)
		return
	}
	sessionName := fmt.Sprintf("%s-%d", t.ID, seq)
	logFile := agent.LogPath(root, t.ID, runID)

	if runner.Alive(sessionName) {
		log.Printf("%s/%s: session %s already exists, skipping", t.ID, runID, sessionName)
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

	// Give Claude Code a deterministic session id so we can find its
	// transcript at ~/.claude/projects/<encoded-cwd>/<uuid>.jsonl
	// after the run and pull the plan file path out of it.
	var sessionUUID string
	if ac.Command == "claude" {
		id, err := agent.NewSessionID()
		if err != nil {
			log.Printf("%s/%s: failed to generate session id: %v", t.ID, runID, err)
		} else {
			sessionUUID = id
			argv = append([]string{"--session-id", sessionUUID}, argv...)
		}
	}

	// Write "spawned" status before starting the session.
	now := time.Now().UTC().Truncate(time.Second)
	as := agent.AgentStatus{
		TicketID:    t.ID,
		RunID:       runID,
		Seq:         seq,
		Attempt:     attempt,
		Stage:       t.Stage,
		Agent:       ac.Command,
		Session:     sessionName,
		Status:      agent.StatusSpawned,
		SpawnedAt:   now,
		LogFile:     logFile,
		Worktree:    wtPath,
		SessionUUID: sessionUUID,
	}
	if err := agent.Write(root, as); err != nil {
		log.Printf("%s/%s: failed to write agent status: %v", t.ID, runID, err)
		return
	}

	// Write agent info into the ticket frontmatter so Obsidian users
	// can see it. This happens before the session starts, so there is
	// no concurrent write with the agent.
	t.AgentStatus = string(agent.StatusRunning)
	t.AgentRun = runID
	t.AgentSession = sessionName
	t.UpdatedAt = time.Now().UTC().Truncate(time.Second)
	if err := t.WriteFile(); err != nil {
		log.Printf("%s/%s: failed to update ticket frontmatter: %v", t.ID, runID, err)
	}

	if err := os.MkdirAll(agent.RunsDir(root, t.ID), 0o755); err != nil {
		log.Printf("%s/%s: failed to create runs dir: %v", t.ID, runID, err)
		return
	}

	// Pin the starting directory: worktree if configured, otherwise
	// the repo root. This keeps Claude Code's transcript path
	// (~/.claude/projects/<encoded-cwd>/…) deterministic so
	// waitForSession can find it after the run.
	cwd := wtPath
	if cwd == "" {
		cwd = root
	}

	// Build full argv: command + args.
	fullArgv := append([]string{ac.Command}, argv...)

	if err := runner.Start(sessionName, cwd, fullArgv, logFile); err != nil {
		log.Printf("%s �� %s: failed to start agent session: %v", t.ID, t.Stage, err)
		as.Status = agent.StatusErrored
		as.Error = err.Error()
		agent.Write(root, as) // best-effort
		t.AgentStatus = string(agent.StatusErrored)
		t.AgentSession = ""
		t.WriteFile() // best-effort
		return
	}

	wtInfo := ""
	if wtPath != "" {
		wtInfo = fmt.Sprintf(" [worktree: %s]", worktree.BranchPrefix+t.ID)
	}
	attemptInfo := ""
	if attempt > 1 {
		attemptInfo = fmt.Sprintf(" (attempt %d)", attempt)
	}
	log.Printf("%s → %s%s: agent running (view with: tickets agents log %s)%s", t.ID, t.Stage, attemptInfo, t.ID, wtInfo)

	mon.TrackRun(t.ID, runID)
	go waitForSession(t, runID, ac.Command, sessionName, root, mon, runner)
}

// waitForSession blocks until the PTY session exits and updates the
// run's status file.
func waitForSession(t ticket.Ticket, runID, agentName, sessionName, root string, mon *agent.Monitor, runner *agent.PTYRunner) {
	defer mon.UntrackRun(t.ID, runID)

	exitCode, waitErr := runner.Wait(sessionName)

	log.Printf("%s/%s: agent %s finished (session %s closed)", t.ID, runID, agentName, sessionName)

	finalStatus := agent.StatusDone
	var statusErr string

	if waitErr != nil {
		finalStatus = agent.StatusFailed
		statusErr = fmt.Sprintf("session error: %v", waitErr)
	} else if exitCode != nil && *exitCode != 0 {
		finalStatus = agent.StatusFailed
		statusErr = fmt.Sprintf("agent exited with code %d", *exitCode)
	}

	// Update run file — skip if handleRemove already set a terminal
	// state (resolves the race where both paths try to update after
	// the session closes).
	if as, err := agent.ReadRun(root, t.ID, runID); err == nil && !as.Status.IsTerminal() {
		as.Status = finalStatus
		as.ExitCode = exitCode
		as.Error = statusErr
		as.PlanFile = lookupPlanFile(as, root)
		if werr := agent.Write(root, as); werr != nil {
			log.Printf("%s/%s: failed to update agent status: %v", t.ID, runID, werr)
		}
	}

	// Update ticket frontmatter with the final agent status. The agent
	// has exited so there is no concurrent write risk. Reload the
	// ticket fresh because the agent likely modified its body.
	if store, err := ticket.Open(root); err == nil {
		if fresh, err := store.Get(t.ID); err == nil {
			fresh.AgentStatus = string(finalStatus)
			fresh.AgentRun = runID
			fresh.AgentSession = ""
			if err := store.Save(fresh); err != nil {
				log.Printf("%s/%s: failed to update ticket frontmatter: %v", t.ID, runID, err)
			}
		}
	}
}

// backfillPlanFiles walks every recorded run and, for runs that have
// a session id but no recorded plan path, re-runs the transcript
// lookup and persists whatever it finds. Runs with a plan path
// already set are left alone.
func backfillPlanFiles(root string) {
	runs, err := agent.ListAll(root)
	if err != nil {
		log.Printf("backfill plans: list runs: %v", err)
		return
	}
	for _, as := range runs {
		if as.SessionUUID == "" || as.PlanFile != "" {
			continue
		}
		path := lookupPlanFile(as, root)
		if path == "" {
			continue
		}
		if err := agent.SetPlanFile(root, as.TicketID, as.RunID, path); err != nil {
			log.Printf("backfill plans: %s/%s: %v", as.TicketID, as.RunID, err)
			continue
		}
		log.Printf("backfill plans: %s/%s → %s", as.TicketID, as.RunID, path)
	}
}

// lookupPlanFile returns the path of the plan file Claude Code wrote
// during this run (if any) by parsing its session transcript. An empty
// string means no plan was produced, no Claude session id was recorded,
// or the transcript could not be read.
func lookupPlanFile(as agent.AgentStatus, root string) string {
	if as.SessionUUID == "" {
		return ""
	}
	cwd := as.Worktree
	if cwd == "" {
		cwd = root
	}
	transcript, err := agent.ClaudeTranscriptPath(as.SessionUUID, cwd)
	if err != nil {
		return ""
	}
	planFile, err := agent.ExtractPlanFromTranscript(transcript)
	if err != nil {
		return ""
	}
	return planFile
}

// --- terminal server discovery ---

func terminalServerFilePath(root string) string {
	return filepath.Join(root, config.ConfigDir, ".terminal-server")
}

func writeTerminalServerFile(root string, port int) {
	data, _ := json.Marshal(map[string]int{"pid": os.Getpid(), "port": port})
	os.WriteFile(terminalServerFilePath(root), data, 0o644)
}

func removeTerminalServerFile(root string) {
	os.Remove(terminalServerFilePath(root))
}

// --- shared ---

// ansiRegex matches ANSI escape sequences (colors, cursor movement,
// etc.) captured from the raw PTY output.
var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\x1b\].*?\x07|\x1b\[.*?[HJK]`)

func stripAnsi(s string) string {
	return ansiRegex.ReplaceAllString(s, "")
}

