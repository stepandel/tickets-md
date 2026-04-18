package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"charm.land/huh/v2"
	"github.com/spf13/cobra"

	"github.com/stepandel/tickets-md/internal/userconfig"
)

func newEditCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "edit <id>",
		Short: "Open a ticket in your editor",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStoreAuto(cmd)
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
			if err := c.Run(); err != nil {
				return err
			}

			// Auto-fix broken links after manual edits.
			issues, derr := s.DoctorTicket(t.ID, false)
			if derr != nil {
				fmt.Fprintf(os.Stderr, "warning: doctor: %v\n", derr)
				return nil
			}
			for _, issue := range issues {
				fmt.Println(issue.String())
			}
			return nil
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
//  4. If stdin is a TTY: huh picker → save to user config
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
	command string
	label   string
}

var allEditorCandidates = []editorCandidate{
	{"code", "VS Code"},
	{"cursor", "Cursor"},
	{"nvim", "Neovim"},
	{"vim", "Vim"},
	{"nano", "Nano"},
	{"vi", "Vi"},
}

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

// runEditorWizard prompts the user to pick an editor via a huh
// Select picker. If the user picks "Other...", it shows a text
// input for a custom command.
func runEditorWizard() (string, error) {
	available := detectAvailableEditors()

	opts := make([]huh.Option[string], 0, len(available)+1)
	for _, c := range available {
		opts = append(opts, huh.NewOption(
			fmt.Sprintf("%-12s (%s)", c.command, c.label),
			c.command,
		))
	}
	opts = append(opts, huh.NewOption("Other...", "other"))

	var choice string
	err := huh.NewSelect[string]().
		Title("Choose your editor").
		Description("tickets will save your choice so it only asks this once").
		Options(opts...).
		Value(&choice).
		Run()
	if err != nil {
		return "", err
	}

	if choice != "other" {
		return choice, nil
	}

	// "Other..." selected — prompt for a custom command.
	var custom string
	err = huh.NewInput().
		Title("Editor command").
		Description("e.g. 'subl -w', 'nvim', 'code --wait'").
		Value(&custom).
		Validate(func(s string) error {
			if strings.TrimSpace(s) == "" {
				return errors.New("command is required")
			}
			return nil
		}).
		Run()
	if err != nil {
		return "", err
	}

	bin := strings.Fields(custom)[0]
	if _, err := exec.LookPath(bin); err != nil {
		fmt.Printf("  warning: %q not found on PATH — saving anyway\n", bin)
	}
	return custom, nil
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
