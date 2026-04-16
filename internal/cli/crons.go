package cli

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/stepandel/tickets-md/internal/agent"
)

func newCronsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "crons",
		Short: "Inspect configured cron agents",
	}
	cmd.AddCommand(newCronsListCmd())
	cmd.AddCommand(newCronsRunCmd())
	cmd.AddCommand(newCronsLogCmd())
	return cmd
}

func newCronsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List configured cron agents and their last run",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			if len(s.Config.CronAgents) == 0 {
				fmt.Println("No cron agents configured.")
				return nil
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintf(tw, "NAME\tSCHEDULE\tENABLED\tLAST RUN\tSTATUS\tELAPSED\n")
			for _, ca := range s.Config.CronAgents {
				lastRun := "-"
				status := "-"
				elapsed := "-"
				if as, err := agent.CronLatest(s.Root, ca.Name); err == nil {
					lastRun = as.RunID
					status = string(as.Status)
					elapsed = time.Since(as.SpawnedAt).Truncate(time.Second).String()
				}
				fmt.Fprintf(tw, "%s\t%s\t%t\t%s\t%s\t%s\n", ca.Name, ca.Schedule, ca.IsEnabled(), lastRun, status, elapsed)
			}
			return tw.Flush()
		},
	}
}

func newCronsLogCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "log <name> [run-id]",
		Short: "Print output for a cron agent run",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			name := args[0]
			var as agent.AgentStatus
			if len(args) == 2 {
				as, err = agent.CronReadRun(s.Root, name, args[1])
				if err != nil {
					return fmt.Errorf("no run %s for cron %s: %w", args[1], name, err)
				}
			} else {
				as, err = agent.CronLatest(s.Root, name)
				if err != nil {
					return fmt.Errorf("no agent runs for cron %s: %w", name, err)
				}
			}
			if as.LogFile == "" {
				return fmt.Errorf("no log file recorded for %s/%s", name, as.RunID)
			}
			data, err := os.ReadFile(as.LogFile)
			if err != nil {
				return fmt.Errorf("reading log: %w", err)
			}
			output := strings.TrimSpace(agent.StripAnsi(string(data)))
			if output == "" {
				fmt.Println("(empty log)")
				return nil
			}
			fmt.Println(output)
			return nil
		},
	}
}

func newCronsRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run <name>",
		Short: "Run a configured cron agent now",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			name := args[0]
			found := false
			for _, ca := range s.Config.CronAgents {
				if ca.Name == name {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("cron %q not found — add it to config.yml and restart `tickets watch`", name)
			}

			session, err := postTerminalServer(s.Root, "/run-cron-agent", map[string]string{"name": name})
			if err != nil {
				var serverErr *terminalServerError
				if errors.As(err, &serverErr) {
					if serverErr.StatusCode == http.StatusConflict {
						return fmt.Errorf("another run is already active")
					}
				}
				return err
			}

			fmt.Printf("fired cron %s (session %s)\n", name, session)
			return nil
		},
	}
}
