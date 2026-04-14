package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func newCompletionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                   "completion <bash|zsh|fish|powershell>",
		Short:                 "Generate shell completion script",
		DisableFlagsInUseLine: true,
		Long: `Generate a shell completion script for tickets.

To load completions for the current shell session:

  bash:
    source <(tickets completion bash)

  zsh:
    source <(tickets completion zsh)

  fish:
    tickets completion fish | source

To load completions for every new shell, redirect the output to a
file your shell reads on startup. For example:

  bash:
    tickets completion bash > /etc/bash_completion.d/tickets

  zsh:
    tickets completion zsh > "${fpath[1]}/_tickets"

  fish:
    tickets completion fish > ~/.config/fish/completions/tickets.fish
`,
		ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
		Args:      cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return cmd.Root().GenBashCompletionV2(os.Stdout, true)
			case "zsh":
				return cmd.Root().GenZshCompletion(os.Stdout)
			case "fish":
				return cmd.Root().GenFishCompletion(os.Stdout, true)
			case "powershell":
				return cmd.Root().GenPowerShellCompletionWithDesc(os.Stdout)
			}
			return fmt.Errorf("unsupported shell %q", args[0])
		},
	}
	return cmd
}
