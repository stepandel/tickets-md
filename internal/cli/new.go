package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func newNewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "new <title...>",
		Short: "Create a new ticket in the default stage",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			title := strings.Join(args, " ")
			t, err := s.Create(title)
			if err != nil {
				return err
			}
			fmt.Printf("Created %s in %s\n  %s\n", t.ID, t.Stage, t.Path)
			return nil
		},
	}
	return cmd
}
