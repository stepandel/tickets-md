package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"tickets-md/internal/userconfig"
)

func newEditCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "edit <id>",
		Short: "Open a ticket in your editor",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			t, err := s.Get(args[0])
			if err != nil {
				return err
			}
			name, editorArgs, err := resolveEditor(os.Stdin, os.Stdout)
			if err != nil {
				return err
			}
			argv := make([]string, 0, len(editorArgs)+1)
			argv = append(argv, editorArgs...)
			argv = append(argv, t.Path)
			c := exec.Command(name, argv...)
			c.Stdin = os.Stdin
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr
			return c.Run()
		},
	}
	return cmd
}

// resolveEditor decides which editor to launch for `tickets edit`.
//
// Order of precedence:
//  1. $VISUAL  — POSIX convention: full-screen editor, preferred
//  2. $EDITOR  — POSIX convention: line editor, fallback
//  3. The user-level config at ~/.config/tickets/config.yml
//  4. If stdin is a TTY: prompt the user once, save to user config
//  5. If stdin is not a TTY: error out (scripts must set $EDITOR)
//
// Step 4 is the "lazy first-run" path: a user who has never set
// $EDITOR is asked exactly once, their choice is persisted, and
// every subsequent `tickets edit` finds it via step 3.
func resolveEditor(in io.Reader, out io.Writer) (string, []string, error) {
	for _, env := range []string{"VISUAL", "EDITOR"} {
		if v := strings.TrimSpace(os.Getenv(env)); v != "" {
			return splitEditorCommand(v)
		}
	}

	uc, _, err := userconfig.Load()
	if err != nil {
		return "", nil, fmt.Errorf("reading user config: %w", err)
	}
	if uc.Editor != "" {
		return splitEditorCommand(uc.Editor)
	}

	if !isTerminal(os.Stdin) {
		return "", nil, errors.New(
			"no editor configured: set $EDITOR, or run `tickets edit` interactively " +
				"once to pick one")
	}

	choice, err := runEditorWizard(in, out)
	if err != nil {
		return "", nil, err
	}
	// Best-effort persistence: if the save fails (e.g. permission
	// denied on ~/.config), warn but still honor the choice for
	// this run so the user isn't blocked.
	if err := userconfig.Save(userconfig.UserConfig{Editor: choice}); err != nil {
		fmt.Fprintf(out, "warning: couldn't save editor preference: %v\n", err)
	} else if p, perr := userconfig.Path(); perr == nil {
		fmt.Fprintf(out, "Saved %q to %s\n\n", choice, p)
	}
	return splitEditorCommand(choice)
}

// editorCandidate is one entry in the auto-detected list of editors.
type editorCandidate struct {
	command string // full command including arguments, e.g. "code -w"
	label   string // human-friendly name shown in the wizard
}

// allEditorCandidates is the canonical ordered list the wizard
// considers. Entries earlier in the list are preferred when present.
// Modern → classic so a developer with both `code` and `vim` sees
// VS Code at the top of the menu.
var allEditorCandidates = []editorCandidate{
	{"code -w", "VS Code"},
	{"cursor -w", "Cursor"},
	{"nvim", "Neovim"},
	{"vim", "Vim"},
	{"nano", "Nano"},
	{"vi", "Vi"},
}

// detectAvailableEditors filters allEditorCandidates down to the
// ones whose binary is actually on PATH, so the wizard never offers
// an editor the user can't launch.
func detectAvailableEditors() []editorCandidate {
	var available []editorCandidate
	for _, c := range allEditorCandidates {
		bin := strings.Fields(c.command)[0]
		if _, err := exec.LookPath(bin); err == nil {
			available = append(available, c)
		}
	}
	return available
}

// runEditorWizard prompts the user to pick an editor on first run.
// It returns the chosen command string (e.g. "code -w") so the
// caller can persist it and parse it. Takes io.Reader/io.Writer for
// testability — production callers pass os.Stdin and os.Stdout.
func runEditorWizard(in io.Reader, out io.Writer) (string, error) {
	available := detectAvailableEditors()

	fmt.Fprintln(out, "You haven't picked an editor yet for `tickets edit`.")
	fmt.Fprintln(out, "tickets will save your choice so it only asks this once.")
	fmt.Fprintln(out)

	if len(available) > 0 {
		fmt.Fprintln(out, "Editors found on this machine:")
		for i, c := range available {
			fmt.Fprintf(out, "  %d. %-12s (%s)\n", i+1, c.command, c.label)
		}
		fmt.Fprintln(out)
	} else {
		fmt.Fprintln(out, "(no known editors found on PATH)")
		fmt.Fprintln(out)
	}

	r := bufio.NewReader(in)
	for {
		if len(available) > 0 {
			fmt.Fprintf(out, "Choose [1-%d], or type a custom command: ", len(available))
		} else {
			fmt.Fprint(out, "Type a command (e.g. 'subl -w'): ")
		}
		line, err := readLine(r)
		if err != nil {
			return "", err
		}
		if line == "" {
			fmt.Fprintln(out, "  (please pick one or type a command)")
			continue
		}
		// Numeric → menu pick.
		if n, perr := strconv.Atoi(line); perr == nil {
			if n < 1 || n > len(available) {
				fmt.Fprintf(out, "  out of range; pick 1-%d\n", len(available))
				continue
			}
			return available[n-1].command, nil
		}
		// Non-numeric → custom command. Validate that the first
		// token is on PATH; warn but accept if not (the user might
		// know something we don't, e.g. an alias they're about to add).
		bin := strings.Fields(line)[0]
		if _, err := exec.LookPath(bin); err != nil {
			fmt.Fprintf(out, "  warning: %q not found on PATH — saving anyway\n", bin)
		}
		return line, nil
	}
}

// splitEditorCommand parses a value like "code -w" into a command
// path and its argv. Whitespace splitting only — no shell quoting.
func splitEditorCommand(s string) (string, []string, error) {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return "", nil, fmt.Errorf("editor command is blank")
	}
	return fields[0], fields[1:], nil
}
