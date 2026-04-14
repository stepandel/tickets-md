package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

func newDoctorCmd() *cobra.Command {
	var dryRun, auto bool
	var staleAfter time.Duration
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Find and fix drift across tickets, agent runs, and worktrees",
		Long: `doctor is the harness's offline GC pass. It scans for drift
that the live watcher might not catch, and fixes it by default.

Checks performed:

  Link integrity (ticket.Store.Doctor):
    - Dangling references to tickets that no longer exist
    - One-sided links where the reciprocal is missing
    - Self-referential links

  Harness drift (HarnessDoctor):
    - Stale non-terminal runs → marked failed
    - Orphan .tickets/.agents/<id>/ dirs → removed
    - Orphan <run>.yml.tmp files from interrupted renames → removed
    - Orphan .worktrees/<id>/ dirs → removed
    - Ticket frontmatter that disagrees with the latest run → rewritten

Use --dry-run to preview without changing anything. A run with no
issues prints "Nothing to do." and exits 0.

--auto restricts the pass to the non-destructive subset — frontmatter
drift and orphan .yml.tmp files — and suppresses output. It is the
same pass tickets watch runs at startup so long-lived stores self-heal.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			if auto {
				if dryRun {
					return fmt.Errorf("--auto and --dry-run are mutually exclusive")
				}
				_, err := AutoHeal(s)
				return err
			}
			fix := !dryRun

			linkIssues, err := s.Doctor(dryRun)
			if err != nil {
				return fmt.Errorf("link check: %w", err)
			}
			harnessIssues, err := HarnessDoctor(s, fix, staleAfter)
			if err != nil {
				return fmt.Errorf("harness check: %w", err)
			}

			total := len(linkIssues) + len(harnessIssues)
			if total == 0 {
				fmt.Println("Nothing to do.")
				return nil
			}

			for _, issue := range linkIssues {
				fmt.Println(issue.String())
			}
			for _, issue := range harnessIssues {
				fmt.Println(issue.String())
			}

			fixed := 0
			for _, issue := range linkIssues {
				if issue.Fixed {
					fixed++
				}
			}
			for _, issue := range harnessIssues {
				if issue.Fixed {
					fixed++
				}
			}

			if dryRun {
				fmt.Printf("\n%d issue(s) found (dry run, nothing changed)\n", total)
			} else {
				fmt.Printf("\n%d issue(s) found, %d fixed\n", total, fixed)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "report issues without fixing them")
	cmd.Flags().BoolVar(&auto, "auto", false, "silently apply the safe subset (frontmatter drift, orphan .tmp files)")
	cmd.Flags().DurationVar(&staleAfter, "stale-after", DefaultStaleAfter, "wall-clock age at which a non-terminal run is considered abandoned")
	return cmd
}
