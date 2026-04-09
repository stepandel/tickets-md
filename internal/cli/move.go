package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newMoveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "move <id> <stage>",
		Aliases: []string{"mv"},
		Short:   "Move a ticket to a different stage",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			t, err := s.Move(args[0], args[1])
			if err != nil {
				return err
			}
			fmt.Printf("Moved %s -> %s\n", t.ID, t.Stage)
			return nil
		},
	}
	return cmd
}
