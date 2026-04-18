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
			root, redirected, err := resolveMoveStoreRoot(globalFlags.root)
			if err != nil {
				return err
			}
			s, err := openStoreAt(root)
			if err != nil {
				return err
			}
			if redirected {
				fmt.Fprintf(cmd.ErrOrStderr(), "Using main repo ticket store at %s\n", s.Root)
			}
			t, err := s.Move(args[0], args[1])
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Moved %s -> %s\n", t.ID, t.Stage)
			return nil
		},
	}
	return cmd
}
