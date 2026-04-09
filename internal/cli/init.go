package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"tickets-md/internal/config"
	"tickets-md/internal/ticket"
)

func newInitCmd() *cobra.Command {
	var (
		prefix string
		stages []string
	)
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create a new ticket store in the current directory",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := config.Default()
			if prefix != "" {
				c.Prefix = prefix
			}
			if len(stages) > 0 {
				c.Stages = stages
			}
			s, err := ticket.Init(globalFlags.root, c)
			if err != nil {
				return err
			}
			fmt.Printf("Initialized ticket store at %s\n", s.Root)
			fmt.Printf("  prefix: %s\n  stages: %v\n", s.Config.Prefix, s.Config.Stages)
			return nil
		},
	}
	cmd.Flags().StringVar(&prefix, "prefix", "", "ticket ID prefix (default TIC)")
	cmd.Flags().StringSliceVar(&stages, "stages", nil, "comma-separated list of stage folder names")
	return cmd
}
