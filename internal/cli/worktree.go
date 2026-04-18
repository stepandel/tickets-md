package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/stepandel/tickets-md/internal/worktree"
)

func newWorktreeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "worktree",
		Aliases: []string{"wt"},
		Short:   "Manage per-ticket git worktrees",
	}
	cmd.AddCommand(
		newWorktreeListCmd(),
		newWorktreeOpenCmd(),
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
			s, err := openStoreAuto(cmd)
			if err != nil {
				return err
			}
			layout := worktreeLayout(s.Config)
			infos, err := worktree.List(s.Root, layout)
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

func newWorktreeOpenCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "open <ticket-id>",
		Short: "Open a ticket's worktree in your editor/IDE",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStoreAuto(cmd)
			if err != nil {
				return err
			}
			wtDir := worktreeLayout(s.Config).WorktreePath(s.Root, args[0])
			if _, err := os.Stat(wtDir); err != nil {
				return fmt.Errorf("no worktree for %s (expected %s)", args[0], wtDir)
			}
			name, editorArgs, err := resolveEditor()
			if err != nil {
				return err
			}
			argv := append(editorArgs, wtDir)
			c := exec.Command(name, argv...)
			c.Stdin = os.Stdin
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr
			return c.Run()
		},
	}
}

func newWorktreeCleanCmd() *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "clean [ticket-id...]",
		Short: "Remove worktrees (specific IDs or --all)",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStoreAuto(cmd)
			if err != nil {
				return err
			}
			layout := worktreeLayout(s.Config)
			if all {
				return cleanAllWorktrees(s.Root, layout)
			}
			if len(args) == 0 {
				return fmt.Errorf("specify ticket IDs or use --all")
			}
			for _, id := range args {
				if err := worktree.Remove(s.Root, layout, id); err != nil {
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

func cleanAllWorktrees(root string, layout worktree.Layout) error {
	infos, err := worktree.List(root, layout)
	if err != nil {
		return err
	}
	if len(infos) == 0 {
		fmt.Println("no worktrees to clean")
		return nil
	}
	for _, info := range infos {
		id := filepath.Base(info.Path)
		if err := worktree.Remove(root, layout, id); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", id, err)
		} else {
			fmt.Printf("removed %s\n", id)
		}
	}
	return nil
}
