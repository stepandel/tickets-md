package cli

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/stepandel/tickets-md/internal/ticket"
)

func newListCmd() *cobra.Command {
	var stage, project string
	var archived bool
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
				ts = filterTicketsByProject(ts, project)
				printStage(stage, ts)
				return nil
			}

			grouped, err := s.ListAll()
			if err != nil {
				return err
			}
			for i, st := range visibleStages(s.Config, archived) {
				if i > 0 {
					fmt.Println()
				}
				printStage(st, filterTicketsByProject(grouped[st], project))
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&stage, "stage", "s", "", "only list tickets in this stage")
	cmd.Flags().StringVar(&project, "project", "", "only list tickets assigned to this project; use - for unassigned")
	cmd.Flags().BoolVar(&archived, "archived", false, "include the configured archive stage in the default grouped view")
	return cmd
}

func filterTicketsByProject(tickets []ticket.Ticket, project string) []ticket.Ticket {
	if project == "" {
		return tickets
	}
	filtered := make([]ticket.Ticket, 0, len(tickets))
	for _, t := range tickets {
		switch {
		case project == "-" && t.Project == "":
			filtered = append(filtered, t)
		case project != "-" && t.Project == project:
			filtered = append(filtered, t)
		}
	}
	return filtered
}

// printStage renders a single stage as a header followed by a tab-aligned
// table of its tickets. An empty stage still prints the header so users
// can see all configured columns at a glance.
func printStage(stage string, tickets []ticket.Ticket) {
	fmt.Printf("[%s] (%d)\n", stage, len(tickets))
	if len(tickets) == 0 {
		return
	}
	showLabels := false
	for _, t := range tickets {
		if len(t.Labels) > 0 {
			showLabels = true
			break
		}
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
		if showLabels {
			labels := renderLabelsOrNone(t.Labels)
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\n", t.ID, priority, labels, links, t.Title)
			continue
		}
		fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\n", t.ID, priority, links, t.Title)
	}
	tw.Flush()
}
