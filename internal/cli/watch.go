package cli

import (
	"bytes"
	"fmt"
	"io"
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
the background with the rendered prompt.

Agent output is appended to the ticket file when the agent exits.

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

	// Graceful shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	// Track recently seen files to debounce rapid CREATE+WRITE
	// sequences (e.g. tickets move does rename then save).
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
			// Only react to new files appearing in a stage dir.
			if !event.Has(fsnotify.Create) {
				continue
			}
			// Debounce: skip if we saw this file in the last second.
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

// handleCreate processes a newly arrived file in a stage directory.
// It checks whether the file is a ticket and the stage has an agent.
func handleCreate(s *ticket.Store, stageConfigs map[string]stage.Config, path string) {
	dir := filepath.Dir(path)
	stageName := filepath.Base(dir)

	// Check it matches the ticket filename pattern.
	base := filepath.Base(path)
	if !strings.HasSuffix(base, ".md") {
		return
	}

	sc, ok := stageConfigs[stageName]
	if !ok || !sc.HasAgent() {
		log.Printf("%s → %s (no agent configured)", strings.TrimSuffix(base, ".md"), stageName)
		return
	}

	// Parse the ticket to get metadata for prompt rendering.
	t, err := ticket.LoadFile(path, stageName)
	if err != nil {
		log.Printf("%s: failed to parse ticket: %v", base, err)
		return
	}

	spawnAgent(t, sc)
}

// spawnAgent starts the configured agent in the background. When it
// exits, its output is appended to the ticket file.
func spawnAgent(t ticket.Ticket, sc stage.Config) {
	ac := sc.Agent

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

	cmd := exec.Command(ac.Command, argv...)
	var outBuf bytes.Buffer
	// Stream to both the terminal (real-time) and the buffer (for
	// appending to the ticket file when the agent finishes).
	mw := io.MultiWriter(&outBuf, os.Stdout)
	cmd.Stdout = mw
	cmd.Stderr = mw

	if err := cmd.Start(); err != nil {
		log.Printf("%s → %s: failed to start %s: %v", t.ID, t.Stage, ac.Command, err)
		return
	}

	log.Printf("%s → %s: spawning %s (pid %d)", t.ID, t.Stage, ac.Command, cmd.Process.Pid)

	go func() {
		err := cmd.Wait()
		if err != nil {
			log.Printf("%s: agent %s failed: %v", t.ID, ac.Command, err)
			// Still append output if there is any (may contain
			// useful error context).
		} else {
			log.Printf("%s: agent %s finished", t.ID, ac.Command)
		}

		output := strings.TrimSpace(outBuf.String())
		if output == "" {
			return
		}
		if appendErr := appendAgentOutput(t.Path, ac.Command, output); appendErr != nil {
			log.Printf("%s: failed to append agent output: %v", t.ID, appendErr)
		} else {
			log.Printf("%s: output appended to %s", t.ID, filepath.Base(t.Path))
		}
	}()
}

// appendAgentOutput adds a timestamped section to the end of a
// ticket file with the agent's output.
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
