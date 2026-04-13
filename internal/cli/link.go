package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newLinkCmd() *cobra.Command {
	var blocks bool
	cmd := &cobra.Command{
		Use:   "link <source> <target>",
		Short: "Link two tickets together",
		Long: `link creates a relationship between two tickets.

By default it creates a "related" link (symmetric). With --blocks it
creates a directional "blocked_by" link: <source> blocks <target>.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			linkType := "related"
			if blocks {
				linkType = "blocked_by"
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
			} else {
				fmt.Printf("Linked %s <-> %s (related)\n", args[0], args[1])
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&blocks, "blocks", "b", false, "create a blocks/blocked_by link instead of related")
	return cmd
}

func newUnlinkCmd() *cobra.Command {
	var blocks bool
	cmd := &cobra.Command{
		Use:   "unlink <source> <target>",
		Short: "Remove a link between two tickets",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			linkType := "related"
			if blocks {
				linkType = "blocked_by"
			}
			sourceID, targetID := args[0], args[1]
			if blocks {
				sourceID, targetID = args[1], args[0]
			}
			if err := s.Unlink(sourceID, targetID, linkType); err != nil {
				return err
			}
			fmt.Printf("Unlinked %s — %s\n", args[0], args[1])
			return nil
		},
	}
	cmd.Flags().BoolVarP(&blocks, "blocks", "b", false, "remove a blocks/blocked_by link instead of related")
	return cmd
}
