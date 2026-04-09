package cli

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/term"
)

// runPicker renders a vertical list of options in the terminal and
// lets the user navigate with arrow keys (↑/↓) or j/k, then select
// with Enter. It returns the index of the chosen option, or an error
// on Ctrl+C / Ctrl+D.
//
// The picker enters raw terminal mode so it can read individual
// keystrokes. A signal handler restores the terminal state if the
// process is killed (SIGINT/SIGTERM) while in raw mode.
func runPicker(title string, options []string) (int, error) {
	fd := int(os.Stdin.Fd())
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return -1, err
	}
	defer term.Restore(fd, oldState)

	// Restore the terminal on SIGINT/SIGTERM so the user doesn't end
	// up with a broken shell if they kill the process while the
	// picker is open.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		term.Restore(fd, oldState)
		os.Exit(130)
	}()
	defer signal.Stop(sigCh)

	cur := 0

	// draw renders the full option list. In raw mode the carriage
	// return (\r) is needed because the terminal won't translate \n
	// into \r\n for us.
	draw := func() {
		for i, opt := range options {
			if i == cur {
				fmt.Printf("\r\033[K  \033[36m❯ %s\033[0m\r\n", opt)
			} else {
				fmt.Printf("\r\033[K    %s\r\n", opt)
			}
		}
		// Move cursor back up to the top of the list so the next
		// draw cycle overwrites cleanly.
		fmt.Printf("\033[%dA", len(options))
	}

	// Print title, then hint line below the options.
	fmt.Printf("  %s\r\n", title)
	draw()
	fmt.Printf("\r\033[K\r\n  \033[2m↑/↓ navigate • enter select • ctrl+c cancel\033[0m")
	// Move back up past the hint and the options to redraw position.
	fmt.Printf("\033[%dA", len(options)+1)
	draw()

	buf := make([]byte, 3)
	for {
		n, err := os.Stdin.Read(buf[:])
		if err != nil {
			return -1, err
		}

		switch {
		// Enter — accept selection.
		case n == 1 && buf[0] == 13:
			// Move cursor below the list + hint line.
			fmt.Printf("\033[%dB", len(options)-cur+1)
			fmt.Printf("\r\n")
			return cur, nil

		// Ctrl+C or Ctrl+D — cancel.
		case n == 1 && (buf[0] == 3 || buf[0] == 4):
			fmt.Printf("\033[%dB", len(options)-cur+1)
			fmt.Printf("\r\n")
			return -1, fmt.Errorf("cancelled")

		// j or Down arrow — move down.
		case n == 1 && buf[0] == 'j',
			n == 3 && buf[0] == 27 && buf[1] == 91 && buf[2] == 66:
			if cur < len(options)-1 {
				cur++
			}

		// k or Up arrow — move up.
		case n == 1 && buf[0] == 'k',
			n == 3 && buf[0] == 27 && buf[1] == 91 && buf[2] == 65:
			if cur > 0 {
				cur--
			}
		}

		draw()
	}
}
