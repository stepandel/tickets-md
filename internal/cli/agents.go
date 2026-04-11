package cli

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"tickets-md/internal/agent"
)

func newAgentsCmd() *cobra.Command {
	var all, verbose bool
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
			printAgents(statuses, verbose)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&all, "all", "a", false, "include completed and failed agents")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "show log file paths")
	cmd.AddCommand(newAgentsLogCmd())
	return cmd
}

func newAgentsLogCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "log <ticket-id>",
		Short: "Print agent output for a ticket",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			as, err := agent.Read(s.Root, args[0])
			if err != nil {
				return fmt.Errorf("no agent status for %s: %w", args[0], err)
			}
			if as.LogFile == "" {
				return fmt.Errorf("no log file recorded for %s", args[0])
			}
			data, err := os.ReadFile(as.LogFile)
			if err != nil {
				return fmt.Errorf("reading log: %w", err)
			}
			output := strings.TrimSpace(stripAnsi(string(data)))
			if output == "" {
				fmt.Println("(empty log)")
				return nil
			}
			fmt.Println(output)
			return nil
		},
	}
}

func printAgents(statuses []agent.AgentStatus, verbose bool) {
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	if verbose {
		fmt.Fprintf(tw, "TICKET\tSTAGE\tAGENT\tSTATUS\tELAPSED\tEXIT\tLOG\n")
	} else {
		fmt.Fprintf(tw, "TICKET\tSTAGE\tAGENT\tSTATUS\tELAPSED\tEXIT\n")
	}
	for _, as := range statuses {
		elapsed := time.Since(as.SpawnedAt).Truncate(time.Second)
		exit := "-"
		if as.ExitCode != nil {
			exit = fmt.Sprintf("%d", *as.ExitCode)
		}
		if verbose {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				as.TicketID, as.Stage, as.Agent, as.Status, elapsed, exit, as.LogFile)
		} else {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
				as.TicketID, as.Stage, as.Agent, as.Status, elapsed, exit)
		}
	}
	tw.Flush()
}
