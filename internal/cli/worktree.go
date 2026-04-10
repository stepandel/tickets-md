package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"tickets-md/internal/worktree"
)

func newWorktreeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "worktree",
		Aliases: []string{"wt"},
		Short:   "Manage per-ticket git worktrees",
	}
	cmd.AddCommand(
		newWorktreeListCmd(),
		newWorktreeCleanCmd(),
	)
	return cmd
}

func newWorktreeListCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List active worktrees",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			infos, err := worktree.List(s.Root)
			if err != nil {
				return err
			}
			if len(infos) == 0 {
				fmt.Println("no worktrees")
				return nil
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "TICKET\tBRANCH\tPATH")
			for _, info := range infos {
				ticket := filepath.Base(info.Path)
				fmt.Fprintf(tw, "%s\t%s\t%s\n", ticket, info.Branch, info.Path)
			}
			return tw.Flush()
		},
	}
}

func newWorktreeCleanCmd() *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "clean [ticket-id...]",
		Short: "Remove worktrees (specific IDs or --all)",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			if all {
				return cleanAllWorktrees(s.Root)
			}
			if len(args) == 0 {
				return fmt.Errorf("specify ticket IDs or use --all")
			}
			for _, id := range args {
				if err := worktree.Remove(s.Root, id); err != nil {
					fmt.Fprintf(os.Stderr, "%s: %v\n", id, err)
				} else {
					fmt.Printf("removed %s\n", id)
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&all, "all", "a", false, "remove all worktrees")
	return cmd
}

func cleanAllWorktrees(root string) error {
	infos, err := worktree.List(root)
	if err != nil {
		return err
	}
	if len(infos) == 0 {
		fmt.Println("no worktrees to clean")
		return nil
	}
	for _, info := range infos {
		id := filepath.Base(info.Path)
		if err := worktree.Remove(root, id); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", id, err)
		} else {
			fmt.Printf("removed %s\n", id)
		}
	}
	return nil
}
