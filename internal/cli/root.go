// Package cli wires the cobra command tree that exposes the ticket
// store on the command line. Every subcommand is a thin wrapper around
// the ticket package; a TUI can be added later that drives the same
// store directly.
package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/stepandel/tickets-md/internal/ticket"
)

// rootFlags holds flags shared across subcommands.
type rootFlags struct {
	root         string
	rootExplicit bool
}

var globalFlags rootFlags

// NewRootCmd builds the cobra root command. It is exported so main.go
// can construct and execute it.
func NewRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tickets",
		Short: "A Linear-style ticketing system backed by markdown files",
		Long: `tickets is a tiny ticket tracker where every ticket is a
markdown file with YAML frontmatter, and every stage is a directory.
Move tickets between stages by renaming the file across folders.`,
		SilenceUsage: true,
		Version:      version,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			globalFlags.rootExplicit = cmd.Flags().Changed("root")
			return maybeNagForUpdate(cmd)
		},
	}
	cmd.PersistentFlags().StringVarP(&globalFlags.root, "root", "C", ".",
		"project root containing the .tickets directory")

	cmd.AddCommand(
		newInitCmd(),
		newNewCmd(),
		newProjectsCmd(),
		newListCmd(),
		newLabelsCmd(),
		newArchiveCmd(),
		newShowCmd(),
		newLabelCmd(),
		newUnlabelCmd(),
		newMoveCmd(),
		newEditCmd(),
		newSetCmd(),
		newRmCmd(),
		newLinkCmd(),
		newUnlinkCmd(),
		newCleanupCmd(),
		newDoctorCmd(),
		newWatchCmd(),
		newAgentsCmd(),
		newCronsCmd(),
		newWorktreeCmd(),
		newBoardCmd(),
		newCompletionCmd(),
		newHooksCmd(),
		newObsidianCmd(),
	)
	return cmd
}

// openStore is the helper every subcommand uses to load the store.
// It centralizes the "did you forget to run init?" error message.
func openStore() (*ticket.Store, error) {
	return openStoreAt(globalFlags.root)
}

func openStoreAuto(cmd *cobra.Command) (*ticket.Store, error) {
	root, redirected, err := resolveStoreRoot(globalFlags.root)
	if err != nil {
		return nil, err
	}
	s, err := openStoreAt(root)
	if err != nil {
		return nil, err
	}
	if redirected {
		fmt.Fprintf(cmd.ErrOrStderr(), "Using main repo ticket store at %s\n", s.Root)
	}
	return s, nil
}
