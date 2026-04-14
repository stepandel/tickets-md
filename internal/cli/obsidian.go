package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"tickets-md/internal/obsidian"
)

func newObsidianCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "obsidian",
		Short: "Manage the companion Obsidian plugin bundled with this CLI",
		Long: `The tickets CLI embeds the ` + "`tickets-board`" + ` Obsidian plugin. These
subcommands drop those bundled assets into an Obsidian vault so the
plugin version stays locked to the CLI version it shipped with.

The vault is discovered by walking up from --vault (or --root / $PWD)
for the nearest directory containing a .obsidian/ folder.`,
	}
	cmd.AddCommand(newObsidianInstallCmd(), newObsidianUninstallCmd(), newObsidianStatusCmd())
	return cmd
}

type obsidianFlags struct {
	vault string
}

func (f *obsidianFlags) bind(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.vault, "vault", "",
		"path inside an Obsidian vault (default: auto-detect from --root)")
}

func (f *obsidianFlags) resolveVault() (string, error) {
	start := f.vault
	if start == "" {
		start = globalFlags.root
	}
	return obsidian.DiscoverVault(start)
}

func newObsidianInstallCmd() *cobra.Command {
	var flags obsidianFlags
	var noEnable bool
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Copy the bundled plugin into an Obsidian vault",
		Long: `Writes main.js, manifest.json, and styles.css into
<vault>/.obsidian/plugins/tickets-board/ and (unless --no-enable) adds
the plugin id to community-plugins.json so Obsidian enables it on
next launch.

If the plugin is already installed it is overwritten — the CLI is the
source of truth for this copy.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			vault, err := flags.resolveVault()
			if err != nil {
				return err
			}
			res, err := obsidian.Install(vault, !noEnable)
			if err != nil {
				return err
			}
			return printObsidianInstallResult(cmd.OutOrStdout(), vault, res, noEnable)
		},
	}
	flags.bind(cmd)
	cmd.Flags().BoolVar(&noEnable, "no-enable", false,
		"only copy files — do not touch community-plugins.json")
	return cmd
}

func newObsidianUninstallCmd() *cobra.Command {
	var flags obsidianFlags
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove the plugin from a vault",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			vault, err := flags.resolveVault()
			if err != nil {
				return err
			}
			if err := obsidian.Uninstall(vault); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Removed %s from %s\n", obsidian.PluginID, vault)
			return nil
		},
	}
	flags.bind(cmd)
	return cmd
}

func newObsidianStatusCmd() *cobra.Command {
	var flags obsidianFlags
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Report installed vs. bundled plugin version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			vault, err := flags.resolveVault()
			if err != nil {
				return err
			}
			r, err := obsidian.Status(vault)
			if err != nil {
				return err
			}
			return printObsidianStatus(cmd.OutOrStdout(), r)
		},
	}
	flags.bind(cmd)
	return cmd
}

func printObsidianInstallResult(out io.Writer, vault string, res obsidian.InstallResult, noEnable bool) error {
	if !obsidian.HasBundle() {
		return fmt.Errorf("internal error: Install succeeded without a bundle")
	}
	verb := "Installed"
	if res.PreviousVersion != "" {
		switch {
		case res.PreviousVersion == res.InstalledVersion:
			verb = "Reinstalled"
		default:
			verb = fmt.Sprintf("Upgraded (%s → %s)", res.PreviousVersion, res.InstalledVersion)
		}
	}
	fmt.Fprintf(out, "%s %s %s into %s\n", verb, obsidian.PluginID, res.InstalledVersion, res.Dir)
	switch {
	case noEnable:
		fmt.Fprintln(out, "Skipped community-plugins.json — enable the plugin from Obsidian's Community plugins pane.")
	case res.Enabled:
		fmt.Fprintln(out, "Added to community-plugins.json — Obsidian will load it on next launch.")
	case res.AlreadyEnabled:
		fmt.Fprintln(out, "Already present in community-plugins.json — reload Obsidian to pick up the new build.")
	}
	fmt.Fprintf(out, "Vault: %s\n", vault)
	return nil
}

func printObsidianStatus(out io.Writer, r obsidian.StatusReport) error {
	fmt.Fprintf(out, "Vault:     %s\n", r.Vault)
	fmt.Fprintf(out, "Bundled:   %s\n", describeBundled(r))
	fmt.Fprintf(out, "Installed: %s\n", describeInstalled(r))
	fmt.Fprintf(out, "Enabled:   %s\n", yesNo(r.Enabled))
	if r.Installed && r.BundledAvailable && r.InstalledVersion != r.BundledVersion {
		fmt.Fprintln(out, "")
		fmt.Fprintln(out, "Run `tickets obsidian install` to sync the vault to the bundled version.")
	}
	return nil
}

func describeBundled(r obsidian.StatusReport) string {
	if !r.BundledAvailable {
		return "not embedded in this binary (rebuild with `make plugin-bundle`)"
	}
	return r.BundledVersion
}

func describeInstalled(r obsidian.StatusReport) string {
	if !r.Installed {
		return "not installed"
	}
	return r.InstalledVersion
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
