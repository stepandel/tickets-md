package cli

import (
	"fmt"
	"log"
	"os"
	"time"

	cron "github.com/robfig/cron/v3"

	"github.com/stepandel/tickets-md/internal/agent"
	"github.com/stepandel/tickets-md/internal/config"
	"github.com/stepandel/tickets-md/internal/stage"
	"github.com/stepandel/tickets-md/internal/terminal"
)

type watchCronScheduler struct {
	engine *cron.Cron
}

func startCronScheduler(root string, cfg config.Config, mon *agent.Monitor, runner *agent.PTYRunner) (*watchCronScheduler, error) {
	if !cfg.HasCronAgents() {
		return nil, nil
	}
	engine := cron.New()
	for _, ca := range cfg.CronAgents {
		if !ca.IsEnabled() {
			log.Printf("cron %s: disabled", ca.Name)
			continue
		}
		ca := ca
		if _, err := engine.AddFunc(ca.Schedule, func() {
			if err := spawnCronAgent(root, ca, mon, runner); err != nil {
				log.Printf("cron %s: %v", ca.Name, err)
			}
		}); err != nil {
			return nil, fmt.Errorf("registering cron %s: %w", ca.Name, err)
		}
		log.Printf("cron %s: scheduled %s", ca.Name, ca.Schedule)
	}
	engine.Start()
	return &watchCronScheduler{engine: engine}, nil
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
		log.Printf("cron %s: skipping tick, previous run %s is still active", ca.Name, prev.RunID)
		return nil
	}
	_, err := startCronRun(root, ca, mon, runner, 0, 0)
	return err
}

func runCronAgentManual(root string, ca config.CronAgentConfig, mon *agent.Monitor, runner *agent.PTYRunner, rows, cols uint16) (string, error) {
	if ca.Worktree {
		return "", fmt.Errorf("cron %s: worktree=true is not supported yet", ca.Name)
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
		newArgv, id, err := integ.PrepareArgs(argv)
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

	fullArgv := append([]string{ca.Command}, argv...)
	if err := runner.Start(sessionName, root, fullArgv, logFile, rows, cols); err != nil {
		as.Status = agent.StatusErrored
		as.Error = err.Error()
		_ = agent.Write(root, as)
		return "", fmt.Errorf("starting agent session: %w", err)
	}

	log.Printf("cron %s: firing run %s", ca.Name, runID)
	mon.TrackRun(ownerID, runID)
	go waitForCronSession(ca.Name, runID, ca.Command, sessionName, root, mon, runner)
	return sessionName, nil
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
