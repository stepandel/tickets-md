package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/stepandel/tickets-md/internal/obsidian"
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

// resolveVault picks the vault for read-only commands (status,
// uninstall). An explicit --vault wins; otherwise prefer the
// `.tickets/` directory (the conventional vault for this CLI), and
// fall back to walking up from --root for a pre-existing `.obsidian/`
// the user might have set up themselves.
func (f *obsidianFlags) resolveVault() (string, error) {
	if f.vault != "" {
		return obsidian.DiscoverVault(f.vault)
	}
	if ticketsDir, ok := existingTicketsDir(); ok {
		if vault, err := obsidian.DiscoverVault(ticketsDir); err == nil {
			return vault, nil
		}
	}
	return obsidian.DiscoverVault(globalFlags.root)
}

// ensureVault is the install-time variant. Resolution order:
//  1. --vault <path>: use it as-is, bootstrapping `.obsidian/` if
//     the caller passed a directory that isn't a vault yet.
//  2. A pre-existing `.obsidian/` at or above --root: respect it
//     (the user already opened this repo as a vault).
//  3. Default: install into `<root>/.tickets/`, which requires
//     `tickets init` to have run. The plugin's Kanban view renders
//     `.tickets/` as its stage columns, so the store directory *is*
//     the vault.
func (f *obsidianFlags) ensureVault() (string, bool, error) {
	if f.vault != "" {
		return obsidian.EnsureVault(f.vault)
	}
	if vault, err := obsidian.DiscoverVault(globalFlags.root); err == nil {
		return vault, false, nil
	}
	ticketsDir, ok := existingTicketsDir()
	if !ok {
		return "", false, fmt.Errorf("no .tickets/ directory at %s — run `tickets init` first so the plugin has a board to render, or pass --vault to install into a different vault",
			mustAbs(globalFlags.root))
	}
	return obsidian.EnsureVault(ticketsDir)
}

func existingTicketsDir() (string, bool) {
	p := filepath.Join(globalFlags.root, ".tickets")
	info, err := os.Stat(p)
	if err != nil || !info.IsDir() {
		return "", false
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", false
	}
	return abs, true
}

func mustAbs(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
}

func newObsidianInstallCmd() *cobra.Command {
	var flags obsidianFlags
	var noEnable bool
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Initialize an Obsidian vault inside .tickets/ and install the bundled plugin",
		Long: `Writes main.js, manifest.json, and styles.css into
<vault>/.obsidian/plugins/tickets-board/ and (unless --no-enable) adds
the plugin id to community-plugins.json so Obsidian activates it on
next launch.

The default vault is ` + "`.tickets/`" + ` itself — the plugin's Kanban view
reads the stage folders there as its columns, so opening ` + "`.tickets/`" + `
as an Obsidian vault is the canonical setup. Run ` + "`tickets init`" + `
first so the directory exists.

If you already opened this repo as a vault elsewhere (a ` + "`.obsidian/`" + `
at or above the project root), that vault is reused instead. Pass
--vault to install into an unrelated vault.

If the plugin is already installed it is overwritten — the CLI is the
source of truth for this copy.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			vault, created, err := flags.ensureVault()
			if err != nil {
				return err
			}
			res, err := obsidian.Install(vault, !noEnable)
			if err != nil {
				return err
			}
			res.VaultCreated = created
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
	if res.VaultCreated {
		fmt.Fprintf(out, "Initialized Obsidian vault at %s\n", vault)
	}
	fmt.Fprintf(out, "%s %s %s into %s\n", verb, obsidian.PluginID, res.InstalledVersion, res.Dir)
	switch {
	case noEnable:
		fmt.Fprintln(out, "Skipped community-plugins.json — you'll need to enable the plugin manually (see step 3 below).")
	case res.Enabled:
		fmt.Fprintln(out, "Marked enabled in community-plugins.json — Obsidian will load it once community plugins are turned on.")
	case res.AlreadyEnabled:
		fmt.Fprintln(out, "Already enabled in community-plugins.json — reload Obsidian to pick up the new build.")
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Next steps in Obsidian (Obsidian has no CLI to register a vault; these are manual):")
	fmt.Fprintln(out, "  1. Open Obsidian → \"Open folder as vault\" and pick this exact path:")
	fmt.Fprintf(out, "       %s\n", vault)
	fmt.Fprintln(out, "     Do not pick the repo root — the plugin expects `.tickets/` to be the vault.")
	fmt.Fprintln(out, "  2. Settings → Community plugins → \"Turn on community plugins\"")
	fmt.Fprintln(out, "  3. Under Installed plugins, toggle \"Tickets Board\" on")
	fmt.Fprintln(out, "  4. Cmd+P (Ctrl+P on Linux/Windows) → \"Tickets Board: Open Tickets Board\"")
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
