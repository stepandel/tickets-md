package cli

import (
	"errors"
	"fmt"
	"log"
	"os"
	"reflect"
	"sync"
	"time"

	cron "github.com/robfig/cron/v3"

	"github.com/stepandel/tickets-md/internal/agent"
	"github.com/stepandel/tickets-md/internal/config"
	"github.com/stepandel/tickets-md/internal/stage"
	"github.com/stepandel/tickets-md/internal/terminal"
)

type watchCronScheduler struct {
	mu      sync.Mutex
	engine  *cron.Cron
	root    string
	mon     *agent.Monitor
	runner  *agent.PTYRunner
	entries map[string]cronEntry
}

type cronEntry struct {
	id  cron.EntryID
	cfg config.CronAgentConfig
}

func startCronScheduler(root string, cfg config.Config, mon *agent.Monitor, runner *agent.PTYRunner) (*watchCronScheduler, error) {
	s := &watchCronScheduler{
		root:    root,
		mon:     mon,
		runner:  runner,
		entries: make(map[string]cronEntry),
	}
	if err := s.Reload(cfg); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *watchCronScheduler) ActiveCount() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.entries)
}

func (s *watchCronScheduler) Reload(cfg config.Config) error {
	if s == nil {
		return nil
	}
	if err := config.ValidateCronAgents(cfg.CronAgents); err != nil {
		return err
	}

	wanted := make(map[string]config.CronAgentConfig, len(cfg.CronAgents))
	for _, ca := range cfg.CronAgents {
		if !ca.IsEnabled() {
			log.Printf("cron %s: disabled", ca.Name)
			continue
		}
		wanted[ca.Name] = ca
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for name := range s.entries {
		if _, ok := wanted[name]; ok {
			continue
		}
		s.unregisterEntry(name)
	}

	var errs []error
	for name, ca := range wanted {
		if cur, ok := s.entries[name]; ok && reflect.DeepEqual(cur.cfg, ca) {
			continue
		}
		if _, ok := s.entries[name]; ok {
			s.unregisterEntry(name)
		}
		if err := s.registerEntry(ca); err != nil {
			errs = append(errs, fmt.Errorf("registering cron %s: %w", name, err))
		}
	}

	return errors.Join(errs...)
}

func (s *watchCronScheduler) registerEntry(ca config.CronAgentConfig) error {
	if s.engine == nil {
		s.engine = cron.New()
		s.engine.Start()
	}
	cfg := ca
	warnCronNeedsOneShotArgs(cfg)
	id, err := s.engine.AddFunc(cfg.Schedule, func() {
		if err := spawnCronAgent(s.root, cfg, s.mon, s.runner); err != nil {
			log.Printf("cron %s: %v", cfg.Name, err)
		}
	})
	if err != nil {
		return err
	}
	s.entries[cfg.Name] = cronEntry{id: id, cfg: cfg}
	log.Printf("cron %s: scheduled %s", cfg.Name, cfg.Schedule)
	return nil
}

func warnCronNeedsOneShotArgs(ca config.CronAgentConfig) {
	if ca.Interactive {
		log.Printf("cron %s: interactive mode — session stays alive until the user closes it; subsequent ticks are skipped while it is active", ca.Name)
		return
	}
	if len(ca.Args) > 0 {
		return
	}
	if integ, ok := agent.Lookup(ca.Command); ok {
		if _, ok := integ.(agent.CronIntegration); ok {
			return
		}
	}
	log.Printf("cron %s: command %q has no cron-specific integration and no args; configure one-shot/exit flags in cron args if it should exit after each tick", ca.Name, ca.Command)
}

func (s *watchCronScheduler) unregisterEntry(name string) {
	if s.engine != nil {
		if entry, ok := s.entries[name]; ok {
			s.engine.Remove(entry.id)
		}
	}
	delete(s.entries, name)
}

func (s *watchCronScheduler) Stop() {
	if s == nil || s.engine == nil {
		return
	}
	ctx := s.engine.Stop()
	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
		log.Printf("cron scheduler: stop timed out waiting for running jobs")
	}
}

func buildCronAgentArgs(ca config.CronAgentConfig, root string) []string {
	prompt := stage.RenderCronPrompt(ca.Prompt, stage.CronPromptVars{
		Root: root,
		Name: ca.Name,
		Now:  time.Now().UTC().Format(time.RFC3339),
	})
	argv := make([]string, 0, len(ca.Args)+1)
	argv = append(argv, ca.Args...)
	argv = append(argv, prompt)
	return argv
}

func spawnCronAgent(root string, ca config.CronAgentConfig, mon *agent.Monitor, runner *agent.PTYRunner) error {
	if ca.Worktree {
		return fmt.Errorf("cron %s: worktree=true is not supported yet", ca.Name)
	}
	if prev, err := agent.CronLatest(root, ca.Name); err == nil && !prev.Status.IsTerminal() && runner.Alive(prev.Session) {
		if ca.Interactive {
			log.Printf("cron %s: skipping tick, previous interactive run %s is still active", ca.Name, prev.RunID)
		} else {
			log.Printf("cron %s: skipping tick, previous run %s is still active", ca.Name, prev.RunID)
		}
		return nil
	}
	paused, err := watchPauseActive(root)
	if err != nil {
		return fmt.Errorf("checking watch pause: %w", err)
	}
	if paused {
		log.Printf("cron %s: watcher paused, skipping run", ca.Name)
		return nil
	}
	_, err = startCronRun(root, ca, mon, runner, 0, 0)
	return err
}

func runCronAgentManual(root string, ca config.CronAgentConfig, mon *agent.Monitor, runner *agent.PTYRunner, rows, cols uint16) (string, error) {
	if ca.Worktree {
		return "", fmt.Errorf("cron %s: worktree=true is not supported yet", ca.Name)
	}
	paused, err := watchPauseActive(root)
	if err != nil {
		return "", fmt.Errorf("checking watch pause: %w", err)
	}
	if paused {
		return "", errWatchPaused
	}
	if prev, err := agent.CronLatest(root, ca.Name); err == nil && !prev.Status.IsTerminal() && runner.Alive(prev.Session) {
		return "", terminal.ErrCronRunActive
	}
	return startCronRun(root, ca, mon, runner, rows, cols)
}

func startCronRun(root string, ca config.CronAgentConfig, mon *agent.Monitor, runner *agent.PTYRunner, rows, cols uint16) (string, error) {
	runID, seq, attempt, err := agent.CronNextRun(root, ca.Name)
	if err != nil {
		return "", fmt.Errorf("computing next run id: %w", err)
	}
	ownerID := agent.CronOwnerID(ca.Name)
	sessionName := fmt.Sprintf(".cron-%s-%d", ca.Name, seq)
	logFile := agent.CronLogPath(root, ca.Name, runID)
	if runner.Alive(sessionName) {
		return sessionName, nil
	}
	if err := os.MkdirAll(agent.CronRunsDir(root, ca.Name), 0o755); err != nil {
		return "", fmt.Errorf("creating runs dir: %w", err)
	}

	argv := buildCronAgentArgs(ca, root)
	var sessionUUID string
	if integ, ok := agent.Lookup(ca.Command); ok {
		prepare := integ.PrepareArgs
		if !ca.Interactive {
			if cronInteg, ok := integ.(agent.CronIntegration); ok {
				prepare = cronInteg.PrepareCronArgs
			}
		}
		newArgv, id, err := prepare(argv)
		if err != nil {
			log.Printf("cron %s/%s: %s integration: %v", ca.Name, runID, ca.Command, err)
		} else {
			argv = newArgv
			sessionUUID = id
		}
	}

	now := time.Now().UTC().Truncate(time.Second)
	as := agent.AgentStatus{
		TicketID:    ownerID,
		RunID:       runID,
		Seq:         seq,
		Attempt:     attempt,
		Stage:       "cron",
		Agent:       ca.Command,
		Session:     sessionName,
		Status:      agent.StatusSpawned,
		SpawnedAt:   now,
		LogFile:     logFile,
		SessionUUID: sessionUUID,
	}
	if err := agent.Write(root, as); err != nil {
		return "", fmt.Errorf("writing agent status: %w", err)
	}
	mon.TrackRun(ownerID, runID)
	tracked := true
	defer func() {
		if tracked {
			mon.UntrackRun(ownerID, runID)
		}
	}()

	fullArgv := append([]string{ca.Command}, argv...)
	if err := runner.Start(sessionName, root, fullArgv, logFile, rows, cols); err != nil {
		as.Status = agent.StatusErrored
		as.Error = err.Error()
		_ = agent.Write(root, as)
		return "", fmt.Errorf("starting agent session: %w", err)
	}

	log.Printf("cron %s: firing run %s", ca.Name, runID)
	tracked = false
	go waitForCronSession(ca.Name, runID, ca.Command, sessionName, root, mon, runner)
	return sessionName, nil
}

func terminateCronSession(root string, ca config.CronAgentConfig, runner *agent.PTYRunner) (string, error) {
	as, err := agent.CronLatest(root, ca.Name)
	if err != nil {
		return "", terminal.ErrCronSessionNotActive
	}
	if as.Status.IsTerminal() || !runner.Alive(as.Session) {
		return "", terminal.ErrCronSessionNotActive
	}
	if err := runner.Kill(as.Session); err != nil {
		return "", terminal.ErrCronSessionNotActive
	}

	latest, err := agent.ReadRun(root, agent.CronOwnerID(ca.Name), as.RunID)
	if err != nil || latest.Status.IsTerminal() {
		return as.Session, nil
	}
	latest.Status = agent.StatusFailed
	latest.Error = "terminated by user"
	latest.ExitCode = nil
	if err := agent.Write(root, latest); err != nil {
		latest, readErr := agent.ReadRun(root, agent.CronOwnerID(ca.Name), as.RunID)
		if readErr == nil && latest.Status.IsTerminal() {
			return as.Session, nil
		}
		return "", fmt.Errorf("writing terminated status: %w", err)
	}
	return as.Session, nil
}

func waitForCronSession(name, runID, agentName, sessionName, root string, mon *agent.Monitor, runner *agent.PTYRunner) {
	ownerID := agent.CronOwnerID(name)
	defer mon.UntrackRun(ownerID, runID)

	exitCode, waitErr := runner.Wait(sessionName)
	finalStatus := agent.StatusDone
	var statusErr string

	if waitErr != nil {
		finalStatus = agent.StatusFailed
		statusErr = fmt.Sprintf("session error: %v", waitErr)
	} else if exitCode != nil && *exitCode != 0 {
		finalStatus = agent.StatusFailed
		statusErr = fmt.Sprintf("agent exited with code %d", *exitCode)
	}

	if exitCode != nil && *exitCode == -1 {
		if _, ok := waitForConcurrentTerminalStatus(root, ownerID, runID, 200*time.Millisecond); ok {
			log.Printf("cron %s/%s: agent %s finished (session %s closed)", name, runID, agentName, sessionName)
			return
		}
	}

	if as, err := agent.ReadRun(root, ownerID, runID); err == nil && !as.Status.IsTerminal() {
		as.Status = finalStatus
		as.ExitCode = exitCode
		as.Error = statusErr
		as.PlanFile = lookupPlanFile(as, root)
		if werr := agent.Write(root, as); werr != nil {
			log.Printf("cron %s/%s: failed to update agent status: %v", name, runID, werr)
		}
	}
	log.Printf("cron %s/%s: agent %s finished (session %s closed)", name, runID, agentName, sessionName)
}
