package cli

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"tickets-md/internal/ticket"
)

func newListCmd() *cobra.Command {
	var stage string
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List tickets, grouped by stage",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}

			if stage != "" {
				ts, err := s.List(stage)
				if err != nil {
					return err
				}
				printStage(stage, ts)
				return nil
			}

			grouped, err := s.ListAll()
			if err != nil {
				return err
			}
			for i, st := range s.Config.Stages {
				if i > 0 {
					fmt.Println()
				}
				printStage(st, grouped[st])
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&stage, "stage", "s", "", "only list tickets in this stage")
	return cmd
}

// printStage renders a single stage as a header followed by a tab-aligned
// table of its tickets. An empty stage still prints the header so users
// can see all configured columns at a glance.
func printStage(stage string, tickets []ticket.Ticket) {
	fmt.Printf("[%s] (%d)\n", stage, len(tickets))
	if len(tickets) == 0 {
		return
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for _, t := range tickets {
		priority := t.Priority
		if priority == "" {
			priority = "-"
		}
		links := ""
		if n := t.LinkCount(); n > 0 {
			links = fmt.Sprintf("[%d links]", n)
		}
		fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\n", t.ID, priority, links, t.Title)
	}
	tw.Flush()
}
