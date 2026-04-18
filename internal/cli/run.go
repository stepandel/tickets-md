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
given ticket. The ticket's full markdown content is passed as a
context-loading prompt; the agent is told to wait for the user's
first message before acting.

Configure the agent in .tickets/config.yml:

  default_agent:
    command: claude
    args: []   # optional extra flags`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStoreAuto(cmd)
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

			prompt := fmt.Sprintf(`Here is the ticket the user wants to work on. Read it for context, but do not start implementing anything yet — wait for the user's first message describing what they actually want you to do with this ticket.

%s`, string(content))

			// `--` terminates option parsing so the prompt (which may
			// contain leading dashes from markdown or frontmatter) is
			// always treated as a positional arg.
			argv := make([]string, 0, len(da.Args)+2)
			argv = append(argv, da.Args...)
			argv = append(argv, "--", prompt)

			c := exec.Command(da.Command, argv...)
			c.Stdin = os.Stdin
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr
			return c.Run()
		},
	}
}
