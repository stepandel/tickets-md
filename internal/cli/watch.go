package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"

	"github.com/stepandel/tickets-md/internal/agent"
	"github.com/stepandel/tickets-md/internal/config"
	"github.com/stepandel/tickets-md/internal/obsidian"
	"github.com/stepandel/tickets-md/internal/stage"
	"github.com/stepandel/tickets-md/internal/terminal"
	"github.com/stepandel/tickets-md/internal/ticket"
	"github.com/stepandel/tickets-md/internal/worktree"
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
    args: ["--dangerously-skip-permissions"]
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
	cmd.AddCommand(
		newWatchPauseCmd(),
		newWatchResumeCmd(),
		newWatchStatusCmd(),
	)
	return cmd
}

type watchTimings struct {
	PollInterval   time.Duration
	IdleBlockAfter time.Duration
	IdleKillAfter  time.Duration
}

type stageConfigStore struct {
	mu      sync.RWMutex
	configs map[string]stage.Config
}

func newStageConfigStore() *stageConfigStore {
	return &stageConfigStore{configs: make(map[string]stage.Config)}
}

func (s *stageConfigStore) Get(stageName string) (stage.Config, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cfg, ok := s.configs[stageName]
	return cfg, ok
}

func (s *stageConfigStore) Set(stageName string, cfg stage.Config) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.configs[stageName] = cfg
}

func (s *stageConfigStore) Delete(stageName string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.configs, stageName)
}

func stageConfigStatus(cfg stage.Config) string {
	switch {
	case cfg.HasAgent():
		return fmt.Sprintf("agent: %s", cfg.Agent.Command)
	case cfg.HasCleanup():
		return "cleanup only"
	default:
		return "no agent"
	}
}

func isStageConfigPath(path string) bool {
	return filepath.Base(path) == ".stage.yml"
}

func reloadStageConfig(stageConfigs *stageConfigStore, stageName, stageDir string) (stage.Config, error) {
	cfg, err := stage.Load(stageDir)
	if err != nil {
		return stage.Config{}, err
	}
	stageConfigs.Set(stageName, cfg)
	return cfg, nil
}

func watchTimingsForConfig(cfg config.Config) watchTimings {
	timings := watchTimings{
		PollInterval:   agent.DefaultPollInterval,
		IdleBlockAfter: agent.DefaultBlockedIdle,
		IdleKillAfter:  0,
	}
	if cfg.Watch == nil {
		return timings
	}
	if cfg.Watch.PollInterval != nil {
		timings.PollInterval = cfg.Watch.PollInterval.Duration
	}
	if cfg.Watch.IdleBlockAfter != nil {
		timings.IdleBlockAfter = cfg.Watch.IdleBlockAfter.Duration
	}
	if cfg.Watch.IdleKillAfter != nil {
		timings.IdleKillAfter = cfg.Watch.IdleKillAfter.Duration
	}
	return timings
}

func reloadWatchConfig(root string, cfg *config.Config, mon *agent.Monitor, reloadCron func(config.Config) error) (watchTimings, bool, []string, []string, error) {
	newCfg, err := config.Load(root)
	if err != nil {
		return watchTimings{}, false, nil, nil, err
	}
	if err := reloadCron(newCfg); err != nil {
		return watchTimings{}, false, nil, nil, err
	}

	prevStages := append([]string(nil), cfg.Stages...)
	nextStages := append([]string(nil), newCfg.Stages...)
	timings := watchTimingsForConfig(newCfg)
	changed := mon.SetTiming(timings.PollInterval, timings.IdleBlockAfter, timings.IdleKillAfter)
	cfg.Stages = nextStages
	cfg.CompleteStages = append([]string(nil), newCfg.CompleteStages...)
	cfg.ArchiveStage = newCfg.ArchiveStage
	cfg.CronAgents = newCfg.CronAgents
	cfg.Watch = newCfg.Watch
	return timings, changed, prevStages, nextStages, nil
}

func reconcileStageWatchSet(w *fsnotify.Watcher, root string, stageConfigs *stageConfigStore, knownPaths map[string]bool, prev, next []string) (int, int) {
	prevSet := make(map[string]struct{}, len(prev))
	for _, stageName := range prev {
		prevSet[stageName] = struct{}{}
	}
	nextSet := make(map[string]struct{}, len(next))
	for _, stageName := range next {
		nextSet[stageName] = struct{}{}
	}

	removed := 0
	for _, stageName := range prev {
		if _, ok := nextSet[stageName]; ok {
			continue
		}
		removed++
		dir := filepath.Join(root, config.ConfigDir, stageName)
		if err := w.Remove(dir); err != nil {
			log.Printf("stage %s: removing watch for %s: %v", stageName, dir, err)
		}
		stageConfigs.Delete(stageName)
		for path := range knownPaths {
			if filepath.Dir(path) == dir {
				delete(knownPaths, path)
			}
		}
		log.Printf("stage %s: removed from watch set", stageName)
	}

	added := 0
	for _, stageName := range next {
		if _, ok := prevSet[stageName]; ok {
			continue
		}
		added++
		dir := filepath.Join(root, config.ConfigDir, stageName)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Printf("stage %s: creating directory %s: %v", stageName, dir, err)
			continue
		}
		if err := stage.WriteDefault(dir); err != nil {
			log.Printf("stage %s: writing default stage config: %v", stageName, err)
			continue
		}
		sc, err := stage.Load(dir)
		if err != nil {
			log.Printf("stage %s: loading stage config: %v", stageName, err)
			continue
		}
		if err := w.Add(dir); err != nil {
			log.Printf("stage %s: watching %s: %v", stageName, dir, err)
			continue
		}
		stageConfigs.Set(stageName, sc)
		entries, err := os.ReadDir(dir)
		if err != nil {
			log.Printf("stage %s: scanning %s: %v", stageName, dir, err)
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
				continue
			}
			knownPaths[filepath.Join(dir, entry.Name())] = true
		}
		log.Printf("watching %s/ (%s) (added)", stageName, stageConfigStatus(sc))
	}

	return added, removed
}

func runWatch(s *ticket.Store) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("creating watcher: %w", err)
	}
	defer w.Close()

	runner := agent.NewPTYRunner()

	// Start the terminal WebSocket server for live PTY access.
	termSrv := terminal.New(runner, s.Root)
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

	// Start the agent status monitor. The monitor owns the authoritative
	// run YAMLs; frontmatter is a cache it rewrites via OnStatusChange.
	timings := watchTimingsForConfig(s.Config)
	mon := agent.NewMonitor(s.Root, timings.PollInterval, timings.IdleBlockAfter, timings.IdleKillAfter, runner.Alive, runner.IdleSeconds, runner.Kill)
	mon.OnStatusChange = func(ticketID string) {
		syncAgentFrontmatter(s.Root, ticketID)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Reconcile stale statuses from a previous watcher run.
	alive, err := mon.Reconcile()
	if err != nil {
		log.Printf("monitor: startup reconciliation failed: %v", err)
	}

	// Silently apply the safe subset of doctor fixes — frontmatter
	// drift and orphan .yml.tmp files — so long-lived stores heal
	// themselves without a manual `tickets doctor` run. Destructive
	// fixes (stale runs, orphan agent dirs, orphan worktrees) stay
	// behind an explicit invocation.
	if issues, err := AutoHeal(s); err != nil {
		log.Printf("startup auto-heal: %v", err)
	} else if len(issues) > 0 {
		log.Printf("startup auto-heal: fixed %d issue(s)", len(issues))
	}

	// Auto-update the Obsidian plugin if the vault exists and the
	// installed version doesn't match this CLI's version.
	maybeUpdateObsidianPlugin(s.Root)

	// Backfill plan_file for any terminal runs whose session-end
	// capture missed it. Self-heals runs that finished under an
	// older binary or raced transcript flushes.
	backfillPlanFiles(s.Root)
	// PTY sessions don't survive watcher restart (child gets SIGHUP),
	// so alive is always empty here. Kept for structural correctness.
	for _, as := range alive {
		if name, ok := agent.CronName(as.TicketID); ok {
			log.Printf("%s/%s: re-attaching to running cron agent (session %s)", as.TicketID, as.RunID, as.Session)
			mon.TrackRun(as.TicketID, as.RunID)
			go waitForCronSession(name, as.RunID, as.Agent, as.Session, s.Root, mon, runner)
			continue
		}
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
	stageConfigs := newStageConfigStore()
	knownPaths := make(map[string]bool)
	for _, st := range s.Config.Stages {
		dir := filepath.Join(s.Root, config.ConfigDir, st)
		sc, err := stage.Load(dir)
		if err != nil {
			return fmt.Errorf("loading stage config for %s: %w", st, err)
		}
		stageConfigs.Set(st, sc)
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

		log.Printf("watching %s/ (%s)", st, stageConfigStatus(sc))
	}
	log.Printf("monitor: poll=%s idle-block=%s idle-kill=%s", timings.PollInterval, timings.IdleBlockAfter, timings.IdleKillAfter)
	if paused, err := watchPauseActive(s.Root); err != nil {
		log.Printf("watch pause: check failed: %v", err)
	} else if paused {
		state, _, err := readWatchPause(s.Root)
		if err != nil {
			log.Printf("watch pause active (metadata unreadable: %v)", err)
		} else {
			log.Printf("%s", watchPauseSummary(state))
		}
	}
	if err := w.Add(filepath.Join(s.Root, config.ConfigDir)); err != nil {
		return fmt.Errorf("watching %s: %w", filepath.Join(s.Root, config.ConfigDir), err)
	}

	cronScheduler, err := startCronScheduler(s.Root, s.Config, mon, runner)
	if err != nil {
		return err
	}
	configPath := config.Path(s.Root)
	var configReloadTimer *time.Timer
	var configReloadCh <-chan time.Time
	var stageReloadTimer *time.Timer
	var stageReloadCh <-chan time.Time
	pendingStageReloads := make(map[string]string)
	scheduleConfigReload := func() {
		if configReloadTimer == nil {
			configReloadTimer = time.NewTimer(250 * time.Millisecond)
			configReloadCh = configReloadTimer.C
			return
		}
		if !configReloadTimer.Stop() {
			select {
			case <-configReloadTimer.C:
			default:
			}
		}
		configReloadTimer.Reset(250 * time.Millisecond)
	}
	scheduleStageReload := func(stageName, stageDir string) {
		pendingStageReloads[stageName] = stageDir
		if stageReloadTimer == nil {
			stageReloadTimer = time.NewTimer(250 * time.Millisecond)
			stageReloadCh = stageReloadTimer.C
			return
		}
		if !stageReloadTimer.Stop() {
			select {
			case <-stageReloadTimer.C:
			default:
			}
		}
		stageReloadTimer.Reset(250 * time.Millisecond)
	}
	defer func() {
		for _, t := range []*time.Timer{configReloadTimer, stageReloadTimer} {
			if t == nil {
				continue
			}
			if !t.Stop() {
				select {
				case <-t.C:
				default:
				}
			}
		}
	}()

	// Wire the "re-run stage agent" callback now that stage configs are
	// loaded. The terminal server exposes this via POST /rerun-stage-agent
	// so the Obsidian plugin can manually trigger the stage agent.
	termSrv.RerunStageAgent = func(ticketID string, force bool, rows, cols uint16) (string, error) {
		return rerunStageAgent(ticketID, force, s, stageConfigs, mon, runner, rows, cols)
	}
	termSrv.RunCronAgent = func(name string, rows, cols uint16) (string, error) {
		for _, ca := range s.Config.CronAgents {
			if ca.Name == name {
				return runCronAgentManual(s.Root, ca, mon, runner, rows, cols)
			}
		}
		return "", fmt.Errorf("cron %q not configured", name)
	}
	termSrv.WatchStatus = func() (terminal.WatchState, error) {
		return terminalWatchState(s.Root)
	}
	termSrv.PauseWatch = func(reason string) (terminal.WatchState, error) {
		if err := writeWatchPause(s.Root, reason); err != nil {
			return terminal.WatchState{}, fmt.Errorf("pausing watch: %w", err)
		}
		return terminalWatchState(s.Root)
	}
	termSrv.ResumeWatch = func() (terminal.WatchState, error) {
		if err := clearWatchPause(s.Root); err != nil {
			return terminal.WatchState{}, fmt.Errorf("resuming watch: %w", err)
		}
		return terminalWatchState(s.Root)
	}

	log.Println("ready — move tickets between stages to trigger agents (ctrl+c to stop)")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	for {
		select {
		case <-sigCh:
			log.Println("shutting down")
			cronScheduler.Stop()
			// Stop accepting new WebSocket clients before SIGTERM'ing
			// children. Otherwise a client that attaches during the
			// 5-second runner grace can Subscribe to a session that's
			// about to close.
			termSrv.Shutdown(context.Background())
			runner.Shutdown()
			cancel()
			return nil

		case event, ok := <-w.Events:
			if !ok {
				return nil
			}
			if event.Name == configPath {
				scheduleConfigReload()
				continue
			}
			if isStageConfigPath(event.Name) {
				if event.Has(fsnotify.Create) || event.Has(fsnotify.Write) || event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
					scheduleStageReload(filepath.Base(filepath.Dir(event.Name)), filepath.Dir(event.Name))
				}
				continue
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

		case <-configReloadCh:
			timings, changed, prevStages, nextStages, err := reloadWatchConfig(s.Root, &s.Config, mon, cronScheduler.Reload)
			if err != nil {
				log.Printf("config: reload failed: %v", err)
				continue
			}
			added, removed := reconcileStageWatchSet(w, s.Root, stageConfigs, knownPaths, prevStages, nextStages)
			log.Printf("config: reloaded %d cron agents", cronScheduler.ActiveCount())
			if added > 0 || removed > 0 {
				log.Printf("config: stage watch set updated (%d added, %d removed)", added, removed)
			}
			if changed {
				log.Printf("monitor: poll=%s idle-block=%s idle-kill=%s (reloaded)", timings.PollInterval, timings.IdleBlockAfter, timings.IdleKillAfter)
			}

		case <-stageReloadCh:
			pending := pendingStageReloads
			pendingStageReloads = make(map[string]string)
			for stageName, stageDir := range pending {
				if !s.Config.HasStage(stageName) {
					continue
				}
				sc, err := reloadStageConfig(stageConfigs, stageName, stageDir)
				if err != nil {
					log.Printf("%s/.stage.yml: reload failed: %v (keeping previous config)", stageName, err)
					continue
				}
				log.Printf("%s/.stage.yml: reloaded (%s)", stageName, stageConfigStatus(sc))
			}

		case err, ok := <-w.Errors:
			if !ok {
				return nil
			}
			log.Printf("watcher error: %v", err)
		}
	}
}

func terminalWatchState(root string) (terminal.WatchState, error) {
	state, paused, err := readWatchPause(root)
	if err != nil {
		if paused {
			return terminal.WatchState{
				Paused:  true,
				Warning: err.Error(),
			}, nil
		}
		return terminal.WatchState{}, fmt.Errorf("reading watch pause state: %w", err)
	}
	return terminal.WatchState{
		Paused:   paused,
		PausedAt: state.PausedAt,
		Reason:   state.Reason,
	}, nil
}

func handleCreate(s *ticket.Store, stageConfigs *stageConfigStore, path string, mon *agent.Monitor, runner *agent.PTYRunner) {
	dir := filepath.Dir(path)
	stageName := filepath.Base(dir)

	base := filepath.Base(path)
	if !strings.HasSuffix(base, ".md") {
		return
	}
	ticketID := strings.TrimSuffix(base, ".md")

	if s.Config.IsCompleteStage(stageName) {
		unblocked, err := s.CompleteUnblock(ticketID)
		if err != nil {
			log.Printf("%s → %s: unblock failed: %v", ticketID, stageName, err)
		} else if len(unblocked) > 0 {
			log.Printf("%s → %s: unblocked %d dependent(s)", ticketID, stageName, len(unblocked))
		}
	}

	sc, ok := stageConfigs.Get(stageName)
	if !ok {
		log.Printf("%s → %s (no stage config)", ticketID, stageName)
		return
	}

	// Run cleanup actions (worktree/branch removal) inline — no agent needed.
	if sc.HasCleanup() {
		runCleanup(ticketID, sc.Cleanup, s.Root, worktreeLayout(s.Config))
	}

	if !sc.HasAgent() {
		if !sc.HasCleanup() {
			log.Printf("%s → %s (no agent configured)", ticketID, stageName)
		}
		return
	}
	paused, err := watchPauseActive(s.Root)
	if err != nil {
		log.Printf("%s → %s: pause check failed: %v", ticketID, stageName, err)
		return
	}
	if paused {
		log.Printf("%s → %s: watcher paused, skipping agent spawn", ticketID, stageName)
		return
	}

	t, err := ticket.LoadFile(path, stageName)
	if err != nil {
		log.Printf("%s: failed to parse ticket: %v", base, err)
		return
	}

	if _, err := spawnAgent(t, sc, s.Root, worktreeLayout(s.Config), mon, runner, 0, 0); err != nil {
		log.Printf("%s → %s: spawn failed: %v", t.ID, t.Stage, err)
	}
}

// runCleanup performs inline worktree and branch cleanup for a ticket.
func runCleanup(ticketID string, cfg *stage.CleanupConfig, root string, layout worktree.Layout) {
	if cfg.Worktree {
		if err := worktree.Remove(root, layout, ticketID); err != nil {
			log.Printf("%s: cleanup: worktree remove failed: %v", ticketID, err)
		} else {
			log.Printf("%s: cleanup: removed worktree", ticketID)
		}
	}
	if cfg.Branch {
		if err := worktree.DeleteBranch(root, layout, ticketID); err != nil {
			log.Printf("%s: cleanup: branch delete failed: %v", ticketID, err)
		} else {
			log.Printf("%s: cleanup: deleted branch %s", ticketID, layout.Branch(ticketID))
		}
	}
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

// rerunStageAgent is the handler for manual stage-agent re-runs
// triggered by the Obsidian plugin. It mirrors the watcher's handleCreate
// path: looks up the ticket's current stage config, verifies an agent is
// configured, refuses if a run is already active, then calls spawnAgent.
func rerunStageAgent(ticketID string, force bool, s *ticket.Store, stageConfigs *stageConfigStore, mon *agent.Monitor, runner *agent.PTYRunner, rows, cols uint16) (string, error) {
	paused, err := watchPauseActive(s.Root)
	if err != nil {
		return "", fmt.Errorf("checking watch pause: %w", err)
	}
	if paused {
		return "", errWatchPaused
	}
	t, err := s.Get(ticketID)
	if err != nil {
		return "", fmt.Errorf("ticket %s: %w", ticketID, err)
	}
	sc, ok := stageConfigs.Get(t.Stage)
	if !ok || !sc.HasAgent() {
		return "", fmt.Errorf("stage %q has no agent configured", t.Stage)
	}
	if latest, err := agent.Latest(s.Root, ticketID); err == nil {
		if !latest.Status.IsTerminal() && runner.Alive(latest.Session) {
			if !force {
				return "", fmt.Errorf("agent already running (session %s)", latest.Session)
			}
			if err := runner.Kill(latest.Session); err != nil {
				return "", fmt.Errorf("killing active session %s: %w", latest.Session, err)
			}
			if cur, err := agent.ReadRun(s.Root, ticketID, latest.RunID); err == nil && !cur.Status.IsTerminal() {
				cur.Status = agent.StatusFailed
				cur.Error = "superseded by force re-run"
				if err := agent.Write(s.Root, cur); err != nil {
					log.Printf("%s/%s: failed to mark superseded run: %v", ticketID, latest.RunID, err)
				} else {
					syncAgentFrontmatter(s.Root, ticketID)
				}
			}
		}
	}
	return spawnAgent(t, sc, s.Root, worktreeLayout(s.Config), mon, runner, rows, cols)
}

// --- agent spawner ---

func spawnAgent(t ticket.Ticket, sc stage.Config, root string, layout worktree.Layout, mon *agent.Monitor, runner *agent.PTYRunner, rows, cols uint16) (string, error) {
	ac := sc.Agent

	runID, seq, attempt, err := agent.NextRun(root, t.ID, t.Stage)
	if err != nil {
		log.Printf("%s: failed to compute next run id: %v", t.ID, err)
		return "", fmt.Errorf("computing next run id: %w", err)
	}
	sessionName := fmt.Sprintf("%s-%d", t.ID, seq)
	logFile := agent.LogPath(root, t.ID, runID)

	if runner.Alive(sessionName) {
		log.Printf("%s/%s: session %s already exists, skipping", t.ID, runID, sessionName)
		return sessionName, nil
	}

	// Create a git worktree if configured.
	var wtPath string
	if ac.Worktree {
		var err error
		wtPath, err = worktree.Create(root, layout, t.ID, ac.BaseBranch)
		if err != nil {
			log.Printf("%s: failed to create worktree: %v", t.ID, err)
			return "", fmt.Errorf("creating worktree: %w", err)
		}
		worktree.EnsureGitignored(root, layout)
		log.Printf("%s: created worktree at %s (branch %s)", t.ID, wtPath, layout.Branch(t.ID))
	}

	argv := buildAgentArgs(t, ac, wtPath)

	// Let any registered agent integration inject startup flags and
	// return a session id we persist in the run YAML — used later to
	// locate run-produced artifacts like plan files.
	var sessionUUID string
	if integ, ok := agent.Lookup(ac.Command); ok {
		newArgv, id, err := integ.PrepareArgs(argv)
		if err != nil {
			log.Printf("%s/%s: %s integration: %v", t.ID, runID, ac.Command, err)
		} else {
			argv = newArgv
			sessionUUID = id
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
		return "", fmt.Errorf("writing agent status: %w", err)
	}
	mon.TrackRun(t.ID, runID)
	tracked := true
	defer func() {
		if tracked {
			mon.UntrackRun(t.ID, runID)
		}
	}()
	syncAgentFrontmatter(root, t.ID)

	if err := os.MkdirAll(agent.RunsDir(root, t.ID), 0o755); err != nil {
		log.Printf("%s/%s: failed to create runs dir: %v", t.ID, runID, err)
		markSpawnErrored(root, t.ID, runID, fmt.Errorf("creating runs dir: %w", err))
		return "", fmt.Errorf("creating runs dir: %w", err)
	}

	// Pin the starting directory: worktree if configured, otherwise
	// the repo root. Integrations depend on a stable cwd to locate
	// per-run artifacts (e.g. transcript paths derived from cwd).
	cwd := wtPath
	if cwd == "" {
		cwd = root
	}

	// Build full argv: command + args.
	fullArgv := append([]string{ac.Command}, argv...)

	if err := runner.Start(sessionName, cwd, fullArgv, logFile, rows, cols); err != nil {
		log.Printf("%s → %s: failed to start agent session: %v", t.ID, t.Stage, err)
		markSpawnErrored(root, t.ID, runID, err)
		return "", fmt.Errorf("starting agent session: %w", err)
	}

	wtInfo := ""
	if wtPath != "" {
		wtInfo = fmt.Sprintf(" [worktree: %s]", layout.Branch(t.ID))
	}
	attemptInfo := ""
	if attempt > 1 {
		attemptInfo = fmt.Sprintf(" (attempt %d)", attempt)
	}
	log.Printf("%s → %s%s: agent running (view with: tickets agents log %s)%s", t.ID, t.Stage, attemptInfo, t.ID, wtInfo)

	tracked = false
	go waitForSession(t, runID, ac.Command, sessionName, root, mon, runner)
	return sessionName, nil
}

func markSpawnErrored(root, ticketID, runID string, cause error) {
	as, err := agent.ReadRun(root, ticketID, runID)
	if err != nil {
		log.Printf("%s/%s: failed to reload agent status after spawn error: %v", ticketID, runID, err)
		return
	}
	as.Status = agent.StatusErrored
	as.Error = cause.Error()
	if err := agent.Write(root, as); err != nil {
		log.Printf("%s/%s: failed to mark spawn errored: %v", ticketID, runID, err)
	}
	syncAgentFrontmatter(root, ticketID)
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

	// Rewrite frontmatter from the (now-final) run YAML. The YAML is the
	// source of truth; syncAgentFrontmatter projects it onto the md.
	syncAgentFrontmatter(root, t.ID)
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
	cronRuns, err := agent.ListAllCronRuns(root)
	if err == nil {
		runs = append(runs, cronRuns...)
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

// lookupPlanFile returns the path of the plan file produced during
// this run (if any) by delegating to the agent's integration. An
// empty string means no plan was produced, no session id was
// recorded, the agent has no integration registered, or the
// integration could not read its artifact.
func lookupPlanFile(as agent.AgentStatus, root string) string {
	if as.SessionUUID == "" {
		return ""
	}
	integ, ok := agent.Lookup(as.Agent)
	if !ok {
		return ""
	}
	cwd := as.Worktree
	if cwd == "" {
		cwd = root
	}
	planFile, err := integ.ExtractPlan(as.SessionUUID, cwd)
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

// syncAgentFrontmatter rewrites the ticket's agent_* frontmatter fields
// from the latest run YAML. YAMLs are authoritative; frontmatter is a
// cached projection that the Obsidian plugin renders from. Called on
// every run-status change and on watcher startup to heal drift (e.g.
// if the watcher was killed between writing a YAML and writing the md).
func syncAgentFrontmatter(root, ticketID string) {
	if agent.IsCronOwner(ticketID) {
		return
	}
	store, err := ticket.Open(root)
	if err != nil {
		return
	}
	t, err := store.Get(ticketID)
	if err != nil {
		return
	}

	wantStatus, wantRun, wantSession := desiredFrontmatter(root, ticketID)
	if t.AgentStatus == wantStatus && t.AgentRun == wantRun && t.AgentSession == wantSession {
		return
	}
	t.AgentStatus = wantStatus
	t.AgentRun = wantRun
	t.AgentSession = wantSession
	if err := store.Save(t); err != nil {
		log.Printf("%s: failed to sync agent frontmatter: %v", ticketID, err)
	}
}

// maybeUpdateObsidianPlugin checks whether the vault has a plugin
// installed at a different version than the running CLI and silently
// updates it. Errors are logged but never fatal — watch must start
// regardless.
func maybeUpdateObsidianPlugin(root string) {
	if version == "dev" || version == "" {
		return
	}

	vaultStart := filepath.Join(root, config.ConfigDir)
	vault, err := obsidian.DiscoverVault(vaultStart)
	if err != nil {
		return // no vault — nothing to update
	}

	status, err := obsidian.Status(vault, version)
	if err != nil || !status.Installed {
		return
	}
	if status.InstalledVersion == status.ExpectedVersion {
		return
	}

	res, err := obsidian.Install(vault, false, version, "")
	if err != nil {
		log.Printf("obsidian plugin auto-update: %v", err)
		return
	}
	log.Printf("obsidian plugin updated %s → %s (%s)", res.PreviousVersion, res.InstalledVersion, res.Source)
}

// --- shared ---
