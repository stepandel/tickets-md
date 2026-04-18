package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newLinkCmd() *cobra.Command {
	var blocks, parent bool
	cmd := &cobra.Command{
		Use:   "link <source> <target>",
		Short: "Link two tickets together",
		Long: `link creates a relationship between two tickets.

By default it creates a "related" link (symmetric). With --blocks it
creates a directional "blocked_by" link: <source> blocks <target>.
With --parent it creates a parent/child link: <source> is a child of <target>.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStoreAuto(cmd)
			if err != nil {
				return err
			}
			linkType := "related"
			if blocks && parent {
				return fmt.Errorf("choose only one of --blocks or --parent")
			}
			if blocks {
				linkType = "blocked_by"
			}
			if parent {
				linkType = "parent"
			}
			sourceID, targetID := args[0], args[1]
			if blocks {
				// "source blocks target" means target is blocked_by source.
				sourceID, targetID = args[1], args[0]
			}
			if err := s.Link(sourceID, targetID, linkType); err != nil {
				return err
			}
			if blocks {
				fmt.Printf("Linked %s blocks %s\n", args[0], args[1])
			} else if parent {
				fmt.Printf("Linked %s -> parent %s\n", args[0], args[1])
			} else {
				fmt.Printf("Linked %s <-> %s (related)\n", args[0], args[1])
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&blocks, "blocks", "b", false, "create a blocks/blocked_by link instead of related")
	cmd.Flags().BoolVar(&parent, "parent", false, "create a parent/child link where <source> becomes a child of <target>")
	return cmd
}

func newUnlinkCmd() *cobra.Command {
	var blocks, parent bool
	cmd := &cobra.Command{
		Use:   "unlink <source> <target>",
		Short: "Remove a link between two tickets",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStoreAuto(cmd)
			if err != nil {
				return err
			}
			linkType := "related"
			if blocks && parent {
				return fmt.Errorf("choose only one of --blocks or --parent")
			}
			if blocks {
				linkType = "blocked_by"
			}
			if parent {
				linkType = "parent"
			}
			sourceID, targetID := args[0], args[1]
			if blocks {
				sourceID, targetID = args[1], args[0]
			}
			if err := s.Unlink(sourceID, targetID, linkType); err != nil {
				return err
			}
			if parent {
				fmt.Printf("Unlinked %s from parent %s\n", args[0], args[1])
			} else {
				fmt.Printf("Unlinked %s — %s\n", args[0], args[1])
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&blocks, "blocks", "b", false, "remove a blocks/blocked_by link instead of related")
	cmd.Flags().BoolVar(&parent, "parent", false, "remove a parent/child link where <source> is the child of <target>")
	return cmd
}
