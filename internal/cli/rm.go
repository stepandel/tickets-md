package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func newRmCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "rm <id>",
		Short: "Delete a ticket",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			t, err := s.Get(args[0])
			if err != nil {
				return err
			}
			if !force {
				fmt.Printf("Delete %s (%s)? [y/N] ", t.ID, t.Title)
				r := bufio.NewReader(os.Stdin)
				line, _ := r.ReadString('\n')
				if !strings.EqualFold(strings.TrimSpace(line), "y") {
					fmt.Println("aborted")
					return nil
				}
			}
			if err := s.Delete(t.ID); err != nil {
				return err
			}
			fmt.Printf("Deleted %s\n", t.ID)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "skip confirmation prompt")
	return cmd
}
