package cli

import (
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"tickets-md/internal/agent"
)

func newAgentsCmd() *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "agents",
		Short: "List agent statuses",
		Long: `Show the status of agents spawned by tickets watch.
By default only active (non-terminal) agents are shown.
Use --all to include completed and failed agents.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			statuses, err := agent.List(s.Root)
			if err != nil {
				return err
			}
			if !all {
				var active []agent.AgentStatus
				for _, as := range statuses {
					if !as.Status.IsTerminal() {
						active = append(active, as)
					}
				}
				statuses = active
			}
			if len(statuses) == 0 {
				fmt.Println("No agents running.")
				return nil
			}
			printAgents(statuses)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&all, "all", "a", false, "include completed and failed agents")
	return cmd
}

func printAgents(statuses []agent.AgentStatus) {
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "TICKET\tSTAGE\tAGENT\tSTATUS\tELAPSED\tEXIT\n")
	for _, as := range statuses {
		elapsed := time.Since(as.SpawnedAt).Truncate(time.Second)
		exit := "-"
		if as.ExitCode != nil {
			exit = fmt.Sprintf("%d", *as.ExitCode)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			as.TicketID, as.Stage, as.Agent, as.Status, elapsed, exit)
	}
	tw.Flush()
}
