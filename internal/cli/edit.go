package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
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
//  1. $VISUAL (POSIX convention: full-screen editor, preferred)
//  2. $EDITOR (POSIX convention: line editor, fallback)
//  3. The first available command from a small list of well-known
//     editors, so users on a fresh shell without either env var set
//     still get a working `tickets edit` instead of an error.
//
// The env-var path supports a command with arguments like
// `code --wait` by splitting on whitespace. Quoted args are not
// supported — users who need them should point $EDITOR at a wrapper
// script.
func resolveEditor() (string, []string, error) {
	for _, env := range []string{"VISUAL", "EDITOR"} {
		if v := strings.TrimSpace(os.Getenv(env)); v != "" {
			return splitEditorCommand(v)
		}
	}
	// Modern → classic so a dev with both `code` and `vim` installed
	// gets VS Code rather than dropping into vi.
	candidates := []struct {
		name string
		args []string
	}{
		{"code", []string{"-w"}},   // VS Code, -w blocks until the tab closes
		{"cursor", []string{"-w"}}, // Cursor, same flag
		{"nvim", nil},
		{"vim", nil},
		{"nano", nil},
		{"vi", nil},
	}
	for _, c := range candidates {
		if path, err := exec.LookPath(c.name); err == nil {
			return path, c.args, nil
		}
	}
	return "", nil, errors.New("no editor found: set $EDITOR or install one of code, cursor, nvim, vim, nano")
}

// splitEditorCommand parses a value like "code -w" into a command
// path and its argv. Whitespace splitting only — no shell quoting.
func splitEditorCommand(s string) (string, []string, error) {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return "", nil, fmt.Errorf("editor env var is blank")
	}
	return fields[0], fields[1:], nil
}
