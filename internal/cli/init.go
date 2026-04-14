package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/stepandel/tickets-md/internal/config"
	"github.com/stepandel/tickets-md/internal/ticket"
)

func newInitCmd() *cobra.Command {
	var (
		prefix string
		stages []string
	)
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create a new ticket store in the current directory",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := config.Default()
			if prefix != "" {
				c.Prefix = prefix
			}
			switch {
			case len(stages) > 0:
				// Explicit flag wins; no wizard.
				c.Stages = stages
			case isTerminal(os.Stdin):
				// Interactive shell + no flag → ask the user.
				names, err := runStageWizard(os.Stdin, os.Stdout, c.Stages)
				if err != nil {
					return err
				}
				c.Stages = names
			}
			s, err := ticket.Init(globalFlags.root, c)
			if err != nil {
				return err
			}
			fmt.Printf("Initialized ticket store at %s\n", s.Root)
			fmt.Printf("  prefix: %s\n  stages: %v\n", s.Config.Prefix, s.Config.Stages)
			return nil
		},
	}
	cmd.Flags().StringVar(&prefix, "prefix", "", "ticket ID prefix (default TIC)")
	cmd.Flags().StringSliceVar(&stages, "stages", nil, "comma-separated list of stage folder names (skips the wizard)")
	return cmd
}

// isTerminal reports whether f is connected to a character device,
// i.e. an interactive terminal as opposed to a pipe or redirected
// file. Pure stdlib so we don't pull in golang.org/x/term.
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// runStageWizard walks the user through naming the stage folders for
// a brand new ticket store. It first offers the defaults; if the user
// declines, it reads stage names one per line until a blank line is
// submitted. Validation matches config.ValidateStageName so the user
// can't end up with a config that would later be rejected.
func runStageWizard(in io.Reader, out io.Writer, defaults []string) ([]string, error) {
	r := bufio.NewReader(in)

	fmt.Fprintln(out, "Set up the stages for your ticket store.")
	fmt.Fprintf(out, "Defaults: %s\n", strings.Join(defaults, ", "))
	fmt.Fprint(out, "Use defaults? [Y/n]: ")

	answer, err := readLine(r)
	if err != nil {
		return nil, err
	}
	if answer == "" || strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes") {
		return defaults, nil
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, "Enter stage names one at a time. The first stage is the")
	fmt.Fprintln(out, "default for new tickets. Submit a blank line when done.")
	fmt.Fprintln(out)

	var names []string
	seen := make(map[string]struct{})
	i := 1
	for {
		fmt.Fprintf(out, "  Stage %d: ", i)
		name, err := readLine(r)
		if err != nil {
			return nil, err
		}
		if name == "" {
			if len(names) == 0 {
				fmt.Fprintln(out, "    need at least one stage — try again")
				continue
			}
			break
		}
		if err := config.ValidateStageName(name); err != nil {
			fmt.Fprintf(out, "    %s\n", err)
			continue
		}
		if _, dup := seen[name]; dup {
			fmt.Fprintf(out, "    %q already used\n", name)
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
		i++
	}

	fmt.Fprintf(out, "\n%d stages: %s\n\n", len(names), strings.Join(names, " → "))
	return names, nil
}

// readLine reads one line from r, returning the trimmed text. EOF
// before any newline is treated as a cancellation so users can ^D out
// of the wizard cleanly.
func readLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		if errors.Is(err, io.EOF) && line == "" {
			return "", errors.New("wizard cancelled")
		}
		if !errors.Is(err, io.EOF) {
			return "", err
		}
	}
	return strings.TrimSpace(line), nil
}
