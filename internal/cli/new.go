package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func newNewCmd() *cobra.Command {
	var priority string
	var parent string
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
			if priority != "" {
				t.Priority = priority
				if err := s.Save(t); err != nil {
					return err
				}
			}
			if parent != "" {
				if err := s.Link(t.ID, parent, "parent"); err != nil {
					return err
				}
			}
			var extras []string
			if priority != "" {
				extras = append(extras, "priority: "+priority)
			}
			if parent != "" {
				extras = append(extras, "parent: "+parent)
			}
			if len(extras) > 0 {
				fmt.Printf("Created %s in %s (%s)\n  %s\n", t.ID, t.Stage, strings.Join(extras, ", "), t.Path)
			} else {
				fmt.Printf("Created %s in %s\n  %s\n", t.ID, t.Stage, t.Path)
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&priority, "priority", "p", "", "set ticket priority (e.g. low, medium, high, critical)")
	cmd.Flags().StringVar(&parent, "parent", "", "set the new ticket's parent ticket ID")
	return cmd
}
