package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

func newHooksCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hooks",
		Short: "Install helper git hooks alongside the ticket store",
	}
	cmd.AddCommand(newHooksInstallCmd())
	return cmd
}

func newHooksInstallCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install a pre-commit hook (runs make check, plus make plugin-test on plugin changes)",
		Long: `Drops a pre-commit hook into the current git repository's
hooks directory. The hook runs ` + "`make check`" + ` before every commit and
also runs ` + "`make plugin-test`" + ` when staged changes include ` + "`obsidian-plugin/`" + `.

The hook is opt-in: installing is refused if a pre-commit hook
already exists, unless --force is passed.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			hooksDir, err := gitHooksDir(globalFlags.root)
			if err != nil {
				return err
			}
			return installPreCommit(hooksDir, force, cmd.OutOrStdout())
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing pre-commit hook")
	return cmd
}

// gitHooksDir resolves the hooks directory for the repository that
// contains root. It honors `core.hooksPath` if configured, falling
// back to `<git-common-dir>/hooks` otherwise. Returns an error if
// root is not inside a git repository.
func gitHooksDir(root string) (string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if out, err := runGit(absRoot, "config", "--get", "core.hooksPath"); err == nil {
		path := strings.TrimSpace(out)
		if path != "" {
			if !filepath.IsAbs(path) {
				path = filepath.Join(absRoot, path)
			}
			return path, nil
		}
	}
	out, err := runGit(absRoot, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", fmt.Errorf("not a git repository (run this from inside the repo where the hook should live)")
	}
	gitDir := strings.TrimSpace(out)
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(absRoot, gitDir)
	}
	return filepath.Join(gitDir, "hooks"), nil
}

func runGit(cwd string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = cwd
	out, err := cmd.Output()
	return string(out), err
}

const preCommitScript = `#!/bin/sh
# Installed by 'tickets hooks install'. Remove this file to opt out.
# Fails the commit if 'make check' fails.
# Also runs 'make plugin-test' when staged files include obsidian-plugin/.
set -e
make check
if git diff --cached --name-only --diff-filter=ACMR | grep -q '^obsidian-plugin/'; then
	make plugin-test
fi
`

func installPreCommit(hooksDir string, force bool, out io.Writer) error {
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return fmt.Errorf("creating hooks dir: %w", err)
	}
	path := filepath.Join(hooksDir, "pre-commit")

	if _, err := os.Stat(path); err == nil && !force {
		return fmt.Errorf("pre-commit hook already exists at %s (pass --force to overwrite)", path)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if err := os.WriteFile(path, []byte(preCommitScript), 0o755); err != nil {
		return fmt.Errorf("writing hook: %w", err)
	}
	fmt.Fprintf(out, "Installed pre-commit hook at %s\n", path)
	fmt.Fprintln(out, "Runs `make check` before every commit, plus `make plugin-test` when `obsidian-plugin/` files are staged — delete the file to opt out.")
	return nil
}
