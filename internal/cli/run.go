package cli

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
)

func newAgentsRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run <ticket-id>",
		Short: "Start an interactive agent session on a ticket",
		Long: `Launch the default agent in the current terminal for the
given ticket. The ticket's full markdown content (including
frontmatter) is passed as the final argument to the agent command.

Configure the agent in .tickets/config.yml:

  default_agent:
    command: claude
    args: []   # optional extra flags`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}

			if !s.Config.HasDefaultAgent() {
				return fmt.Errorf("no default_agent configured in .tickets/config.yml\n\nAdd:\n\n  default_agent:\n    command: claude")
			}
			da := s.Config.DefaultAgent

			t, err := s.Get(args[0])
			if err != nil {
				return err
			}

			content, err := os.ReadFile(t.Path)
			if err != nil {
				return fmt.Errorf("reading ticket file: %w", err)
			}

			// Terminate option parsing before the prompt: the ticket
			// content starts with `---` frontmatter, which the agent CLI
			// (e.g. claude via commander.js) would otherwise treat as an
			// unknown option and abort.
			argv := make([]string, 0, len(da.Args)+2)
			argv = append(argv, da.Args...)
			argv = append(argv, "--", string(content))

			c := exec.Command(da.Command, argv...)
			c.Stdin = os.Stdin
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr
			return c.Run()
		},
	}
}
