package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/stepandel/tickets-md/internal/agent"
	"github.com/stepandel/tickets-md/internal/ticket"
)

func newRmCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "rm <id>",
		Short: "Delete a ticket",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStoreAuto(cmd)
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
			if err := deleteTicket(s, t.ID, os.Stderr); err != nil {
				return err
			}
			fmt.Printf("Deleted %s\n", t.ID)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "skip confirmation prompt")
	return cmd
}

func deleteTicket(s *ticket.Store, id string, warn io.Writer) error {
	if err := s.Delete(id); err != nil {
		return err
	}
	if err := agent.RemoveTicket(s.Root, id); err != nil {
		fmt.Fprintf(warn, "warning: failed to remove agent data for %s: %v\n", id, err)
	}
	return nil
}
