package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newDoctorCmd() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Find and fix broken ticket links",
		Long: `doctor scans all tickets for link integrity issues:

  - Dangling references to tickets that no longer exist (removed)
  - One-sided links where the reciprocal is missing (added)
  - Self-referential links (removed)

By default it fixes every issue it finds. Use --dry-run to preview
without making changes.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			issues, err := s.Doctor(dryRun)
			if err != nil {
				return err
			}
			if len(issues) == 0 {
				fmt.Println("No link issues found.")
				return nil
			}
			for _, issue := range issues {
				fmt.Println(issue.String())
			}
			if dryRun {
				fmt.Printf("\n%d issue(s) found (dry run, nothing changed)\n", len(issues))
			} else {
				fixed := 0
				for _, issue := range issues {
					if issue.Fixed {
						fixed++
					}
				}
				fmt.Printf("\n%d issue(s) found, %d fixed\n", len(issues), fixed)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "report issues without fixing them")
	return cmd
}
