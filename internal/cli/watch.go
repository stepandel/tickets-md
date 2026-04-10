package cli

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"

	"tickets-md/internal/config"
	"tickets-md/internal/stage"
	"tickets-md/internal/ticket"
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

func runWatch(s *ticket.Store) error {
	if _, err := exec.LookPath("tmux"); err != nil {
		return fmt.Errorf("tmux is required for `tickets watch`: brew install tmux")
	}

	w, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("creating watcher: %w", err)
	}
	defer w.Close()

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
			return nil

		case event, ok := <-w.Events:
			if !ok {
				return nil
			}
			if !event.Has(fsnotify.Create) {
				continue
			}
			if t, ok := seen[event.Name]; ok && time.Since(t) < time.Second {
				continue
			}
			seen[event.Name] = time.Now()

			handleCreate(s, stageConfigs, event.Name)

		case err, ok := <-w.Errors:
			if !ok {
				return nil
			}
			log.Printf("watcher error: %v", err)
		}
	}
}

func handleCreate(s *ticket.Store, stageConfigs map[string]stage.Config, path string) {
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

	spawnAgentTmux(t, sc)
}

// buildAgentArgs returns the full argv (without the command itself)
// for the agent invocation.
func buildAgentArgs(t ticket.Ticket, ac *stage.AgentConfig) []string {
	prompt := stage.RenderPrompt(ac.Prompt, stage.PromptVars{
		Path:  t.Path,
		ID:    t.ID,
		Title: t.Title,
		Stage: t.Stage,
		Body:  t.Body,
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

func spawnAgentTmux(t ticket.Ticket, sc stage.Config) {
	ac := sc.Agent
	argv := buildAgentArgs(t, ac)

	sessionName := t.ID
	logFile := filepath.Join(os.TempDir(), fmt.Sprintf("tickets-%s.log", t.ID))

	// Check for existing session (e.g. ticket moved twice quickly).
	if exec.Command("tmux", "has-session", "-t", sessionName).Run() == nil {
		log.Printf("%s: tmux session already exists, skipping", t.ID)
		return
	}

	// Build a shell command: `<agent> <args> 2>&1 | tee <logfile>`
	parts := []string{shellQuote(ac.Command)}
	for _, a := range argv {
		parts = append(parts, shellQuote(a))
	}
	shellCmd := fmt.Sprintf("%s 2>&1 | tee %s", strings.Join(parts, " "), shellQuote(logFile))

	err := exec.Command("tmux", "new-session", "-d", "-s", sessionName, "sh", "-c", shellCmd).Run()
	if err != nil {
		log.Printf("%s → %s: failed to create tmux session: %v", t.ID, t.Stage, err)
		return
	}

	log.Printf("%s → %s: agent running in tmux (attach with: tmux attach -t %s)", t.ID, t.Stage, sessionName)

	go waitForTmuxSession(t, ac.Command, sessionName, logFile)
}

// waitForTmuxSession polls until the tmux session ends, then reads
// the log file and appends output to the ticket file.
func waitForTmuxSession(t ticket.Ticket, agent, sessionName, logFile string) {
	for {
		time.Sleep(time.Second)
		if exec.Command("tmux", "has-session", "-t", sessionName).Run() != nil {
			break
		}
	}

	log.Printf("%s: agent %s finished (session %s closed)", t.ID, agent, sessionName)

	data, err := os.ReadFile(logFile)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("%s: failed to read agent output: %v", t.ID, err)
		}
		return
	}
	os.Remove(logFile)

	output := strings.TrimSpace(string(data))
	if output == "" {
		return
	}
	if err := appendAgentOutput(t.Path, agent, output); err != nil {
		log.Printf("%s: failed to append agent output: %v", t.ID, err)
	} else {
		log.Printf("%s: output appended to %s", t.ID, filepath.Base(t.Path))
	}
}

// --- shared ---

func appendAgentOutput(path, agent, output string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	now := time.Now().Format("2006-01-02 15:04:05")
	_, err = fmt.Fprintf(f, "\n## Agent Output (%s, %s)\n\n%s\n", agent, now, output)
	return err
}
