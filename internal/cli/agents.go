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
	var all, verbose, history bool
	cmd := &cobra.Command{
		Use:   "agents",
		Short: "List agent statuses",
		Long: `Show the status of agents spawned by tickets watch.

By default only the latest run for each ticket is shown, and only
non-terminal runs. Use --all to include completed and failed runs,
and --history to expand to one row per run instead of just the
latest per ticket.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			if err := agent.MigrateFlat(s.Root); err != nil {
				return err
			}

			var statuses []agent.AgentStatus
			if history {
				statuses, err = agent.ListAll(s.Root)
			} else {
				statuses, err = agent.List(s.Root)
			}
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
	cmd.Flags().BoolVar(&history, "history", false, "show every run, not just the latest per ticket")
	cmd.AddCommand(newAgentsLogCmd())
	return cmd
}

func newAgentsLogCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "log <ticket-id> [run-id]",
		Short: "Print agent output for a ticket",
		Long: `Print the captured output for an agent run.

With just a ticket id, prints the latest run. Pass an explicit run id
(e.g. "002-execute") to inspect an earlier run.`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			if err := agent.MigrateFlat(s.Root); err != nil {
				return err
			}

			ticketID := args[0]
			var as agent.AgentStatus
			if len(args) == 2 {
				as, err = agent.ReadRun(s.Root, ticketID, args[1])
				if err != nil {
					return fmt.Errorf("no run %s for %s: %w", args[1], ticketID, err)
				}
			} else {
				as, err = agent.Latest(s.Root, ticketID)
				if err != nil {
					return fmt.Errorf("no agent runs for %s: %w", ticketID, err)
				}
			}
			if as.LogFile == "" {
				return fmt.Errorf("no log file recorded for %s/%s", ticketID, as.RunID)
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
		fmt.Fprintf(tw, "TICKET\tRUN\tSTAGE\tAGENT\tSTATUS\tELAPSED\tEXIT\tLOG\n")
	} else {
		fmt.Fprintf(tw, "TICKET\tRUN\tSTAGE\tAGENT\tSTATUS\tELAPSED\tEXIT\n")
	}
	for _, as := range statuses {
		elapsed := time.Since(as.SpawnedAt).Truncate(time.Second)
		exit := "-"
		if as.ExitCode != nil {
			exit = fmt.Sprintf("%d", *as.ExitCode)
		}
		runLabel := as.RunID
		if as.Attempt > 1 {
			runLabel = fmt.Sprintf("%s ×%d", as.RunID, as.Attempt)
		}
		if verbose {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				as.TicketID, runLabel, as.Stage, as.Agent, as.Status, elapsed, exit, as.LogFile)
		} else {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				as.TicketID, runLabel, as.Stage, as.Agent, as.Status, elapsed, exit)
		}
	}
	tw.Flush()
}
