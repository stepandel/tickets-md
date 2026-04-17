package cli

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/stepandel/tickets-md/internal/agent"
	"github.com/stepandel/tickets-md/internal/config"
	"github.com/stepandel/tickets-md/internal/ticket"
)

func visibleStages(cfg config.Config, showArchived bool) []string {
	if showArchived || !cfg.HasArchiveStage() {
		return append([]string(nil), cfg.Stages...)
	}
	stages := make([]string, 0, len(cfg.Stages))
	for _, stage := range cfg.Stages {
		if cfg.IsArchiveStage(stage) {
			continue
		}
		stages = append(stages, stage)
	}
	return stages
}

func newArchiveCmd() *cobra.Command {
	var from string
	var olderThan time.Duration
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "archive <id>",
		Short: "Move tickets into the configured archive stage",
		Args: func(cmd *cobra.Command, args []string) error {
			if from != "" {
				if len(args) != 0 {
					return fmt.Errorf("cannot pass <id> together with --from")
				}
				return nil
			}
			if len(args) != 1 {
				return cobra.ExactArgs(1)(cmd, args)
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			if !s.Config.HasArchiveStage() {
				return fmt.Errorf("config archive_stage is not set")
			}
			archiveStage := s.Config.ArchiveStage

			if from == "" {
				return archiveTicket(cmd, s, args[0], archiveStage, dryRun)
			}
			return archiveStageTickets(cmd, s, from, archiveStage, olderThan, dryRun)
		},
	}

	cmd.Flags().StringVar(&from, "from", "", "bulk archive tickets from this source stage")
	cmd.Flags().DurationVar(&olderThan, "older-than", 0, "only archive tickets with updated_at older than this duration")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "report archive moves without performing them")
	return cmd
}

func archiveTicket(cmd *cobra.Command, s *ticket.Store, id, archiveStage string, dryRun bool) error {
	out := cmd.OutOrStdout()
	if dryRun {
		tk, err := s.Get(id)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "Would archive %s: %s -> %s\n", tk.ID, tk.Stage, archiveStage)
		return nil
	}
	tk, err := s.Move(id, archiveStage)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Archived %s -> %s\n", tk.ID, tk.Stage)
	return nil
}

func archiveStageTickets(cmd *cobra.Command, s *ticket.Store, from, archiveStage string, olderThan time.Duration, dryRun bool) error {
	if olderThan <= 0 {
		return fmt.Errorf("--older-than must be greater than zero in bulk mode")
	}
	if s.Config.IsArchiveStage(from) {
		return fmt.Errorf("cannot bulk archive from archive stage %q", from)
	}

	tickets, err := s.List(from)
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	cutoff := time.Now().UTC().Add(-olderThan)
	moved := 0
	skippedActive := 0
	for _, tk := range tickets {
		if !tk.UpdatedAt.Before(cutoff) {
			continue
		}
		latest, err := agent.Latest(s.Root, tk.ID)
		if err == nil && !latest.Status.IsTerminal() {
			skippedActive++
			fmt.Fprintf(out, "Skip %s: latest run %s is %s\n", tk.ID, latest.RunID, latest.Status)
			continue
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}

		if dryRun {
			fmt.Fprintf(out, "Would archive %s: %s -> %s\n", tk.ID, tk.Stage, archiveStage)
			moved++
			continue
		}

		archived, err := s.Move(tk.ID, archiveStage)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "Archived %s -> %s\n", archived.ID, archived.Stage)
		moved++
	}

	if dryRun {
		fmt.Fprintf(out, "%d ticket(s) would be archived", moved)
	} else {
		fmt.Fprintf(out, "%d ticket(s) archived", moved)
	}
	if skippedActive > 0 {
		fmt.Fprintf(out, ", %d skipped with active agent runs", skippedActive)
	}
	fmt.Fprintln(out)
	return nil
}
