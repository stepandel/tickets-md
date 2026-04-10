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

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		term.Restore(fd, oldState)
		os.Exit(130)
	}()
	defer signal.Stop(sigCh)

	cur := 0
	n := len(options)

	// draw overwrites lines 0..N-1 with the current option list,
	// then moves the cursor back to line 0 so the next draw cycle
	// starts in the right place.
	draw := func() {
		for i, opt := range options {
			if i == cur {
				fmt.Printf("\r\033[K  \033[36m❯ %s\033[0m\r\n", opt)
			} else {
				fmt.Printf("\r\033[K    %s\r\n", opt)
			}
		}
		fmt.Printf("\033[%dA", n) // back to line 0
	}

	// --- Screen layout (cursor "line 0" = first option) ---
	//
	//   <title>                              ← printed once, never redrawn
	//   ❯ option 0          ← line 0        ← cursor rests here
	//     option 1          ← line 1
	//     ...
	//     option N-1        ← line N-1
	//   ↑/↓ navigate …     ← line N (hint)  ← printed once

	// Title
	fmt.Printf("  %s\r\n", title)

	// Draw options (cursor ends on line 0)
	draw()

	// Jump past options to print the hint, then jump back.
	fmt.Printf("\033[%dB", n)                                                              // → line N
	fmt.Printf("\r\033[K  \033[2m↑/↓ navigate • enter select • ctrl+c cancel\033[0m") // hint
	fmt.Printf("\033[%dA", n)                                                              // → line 0

	buf := make([]byte, 3)
	for {
		nr, err := os.Stdin.Read(buf[:])
		if err != nil {
			return -1, err
		}

		switch {
		case nr == 1 && buf[0] == 13: // Enter
			fmt.Printf("\033[%dB\r\n", n) // move past hint, clean line
			return cur, nil

		case nr == 1 && (buf[0] == 3 || buf[0] == 4): // Ctrl+C / Ctrl+D
			fmt.Printf("\033[%dB\r\n", n)
			return -1, fmt.Errorf("cancelled")

		case nr == 1 && buf[0] == 'j',
			nr == 3 && buf[0] == 27 && buf[1] == 91 && buf[2] == 66: // Down
			if cur < n-1 {
				cur++
			}

		case nr == 1 && buf[0] == 'k',
			nr == 3 && buf[0] == 27 && buf[1] == 91 && buf[2] == 65: // Up
			if cur > 0 {
				cur--
			}
		}

		draw()
	}
}
