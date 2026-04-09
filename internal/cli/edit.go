package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
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
			name, editorArgs, err := resolveEditor()
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
//  4. If stdin is a TTY: arrow-key picker → save to user config
//  5. If stdin is not a TTY: error out (scripts must set $EDITOR)
func resolveEditor() (string, []string, error) {
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

	choice, err := runEditorWizard()
	if err != nil {
		return "", nil, err
	}
	if err := userconfig.Save(userconfig.UserConfig{Editor: choice}); err != nil {
		fmt.Fprintf(os.Stderr, "warning: couldn't save editor preference: %v\n", err)
	} else if p, perr := userconfig.Path(); perr == nil {
		fmt.Printf("Saved %q to %s\n\n", choice, p)
	}
	return splitEditorCommand(choice)
}

// editorCandidate is one entry in the auto-detected list of editors.
type editorCandidate struct {
	command string // full command including arguments, e.g. "code -w"
	label   string // human-friendly name shown in the picker
}

// allEditorCandidates is the canonical ordered list the picker
// considers. Modern → classic so a developer with both `code` and
// `vim` sees VS Code at the top of the menu.
var allEditorCandidates = []editorCandidate{
	{"code -w", "VS Code"},
	{"cursor -w", "Cursor"},
	{"nvim", "Neovim"},
	{"vim", "Vim"},
	{"nano", "Nano"},
	{"vi", "Vi"},
}

// detectAvailableEditors filters allEditorCandidates down to the
// ones whose binary is actually on PATH.
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

// runEditorWizard prompts the user to pick an editor via an
// interactive arrow-key picker. If the user picks "Other...", it
// falls back to a text prompt for a custom command.
func runEditorWizard() (string, error) {
	available := detectAvailableEditors()

	fmt.Println("You haven't picked an editor yet for `tickets edit`.")
	fmt.Println("tickets will save your choice so it only asks this once.")
	fmt.Println()

	// Build the picker options list, with "Other..." at the end.
	labels := make([]string, 0, len(available)+1)
	for _, c := range available {
		labels = append(labels, fmt.Sprintf("%-12s (%s)", c.command, c.label))
	}
	labels = append(labels, "Other...")

	idx, err := runPicker("Choose your editor:", labels)
	if err != nil {
		return "", err
	}

	// Known editor selected.
	if idx < len(available) {
		return available[idx].command, nil
	}

	// "Other..." selected — prompt for a custom command string.
	fmt.Print("Enter editor command (e.g. 'subl -w'): ")
	r := bufio.NewReader(os.Stdin)
	line, err := readLine(r)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(line) == "" {
		return "", errors.New("no command entered")
	}
	// Warn (but allow) if the binary isn't on PATH.
	bin := strings.Fields(line)[0]
	if _, err := exec.LookPath(bin); err != nil {
		fmt.Printf("  warning: %q not found on PATH — saving anyway\n", bin)
	}
	return line, nil
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
