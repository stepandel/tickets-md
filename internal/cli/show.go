package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func newShowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <id>",
		Short: "Print a ticket's contents",
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
			data, err := os.ReadFile(t.Path)
			if err != nil {
				return err
			}
			fmt.Printf("# %s — %s   [%s]\n", t.ID, t.Title, t.Stage)
			if t.Priority != "" {
				fmt.Printf("# Priority: %s\n", t.Priority)
			}
			if t.Project != "" {
				if p, err := s.GetProject(t.Project); err == nil {
					fmt.Printf("# Project: %s — %s\n", p.ID, p.Title)
				} else {
					fmt.Printf("# Project: %s (missing)\n", t.Project)
				}
			}
			if t.HasLinks() {
				fmt.Printf("# Links: %s\n", t.LinksText())
			}
			fmt.Printf("# %s\n\n", t.Path)
			os.Stdout.Write(data)
			return nil
		},
	}
	return cmd
}
