// Package cli wires the cobra command tree that exposes the ticket
// store on the command line. Every subcommand is a thin wrapper around
// the ticket package; a TUI can be added later that drives the same
// store directly.
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/stepandel/tickets-md/internal/ticket"
)

// rootFlags holds flags shared across subcommands.
type rootFlags struct {
	root string
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
	s, err := ticket.Open(globalFlags.root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no ticket store here — run `tickets init` first")
		}
		return nil, err
	}
	return s, nil
}
