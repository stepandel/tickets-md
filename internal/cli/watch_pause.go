package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/stepandel/tickets-md/internal/config"
)

var errWatchPaused = errors.New("watch paused")

type watchPauseState struct {
	PausedAt time.Time `json:"paused_at"`
	Reason   string    `json:"reason,omitempty"`
}

func watchPausePath(root string) string {
	return filepath.Join(root, config.ConfigDir, ".watch-paused")
}

func watchPauseActive(root string) (bool, error) {
	_, err := os.Stat(watchPausePath(root))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

func readWatchPause(root string) (watchPauseState, bool, error) {
	path := watchPausePath(root)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return watchPauseState{}, false, nil
		}
		return watchPauseState{}, false, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return watchPauseState{}, true, nil
	}

	var state watchPauseState
	if err := json.Unmarshal(data, &state); err != nil {
		return watchPauseState{}, true, err
	}
	return state, true, nil
}

func writeWatchPause(root, reason string) error {
	state := watchPauseState{
		PausedAt: time.Now().UTC().Truncate(time.Second),
		Reason:   strings.TrimSpace(reason),
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}

	path := watchPausePath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func clearWatchPause(root string) error {
	err := os.Remove(watchPausePath(root))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func watchPauseSummary(state watchPauseState) string {
	msg := "watch is paused"
	if !state.PausedAt.IsZero() {
		msg += " since " + state.PausedAt.Format(time.RFC3339)
	}
	if state.Reason != "" {
		msg += " (" + state.Reason + ")"
	}
	return msg
}

func newWatchPauseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pause [reason]",
		Short: "Pause watcher-managed spawns",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStoreAuto(cmd)
			if err != nil {
				return err
			}
			if err := writeWatchPause(s.Root, strings.Join(args, " ")); err != nil {
				return fmt.Errorf("pausing watch: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "watch paused")
			return nil
		},
	}
}

func newWatchResumeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "resume",
		Short: "Resume watcher-managed spawns",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStoreAuto(cmd)
			if err != nil {
				return err
			}
			if err := clearWatchPause(s.Root); err != nil {
				return fmt.Errorf("resuming watch: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "watch resumed")
			return nil
		},
	}
}

func newWatchStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show watcher pause state",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStoreAuto(cmd)
			if err != nil {
				return err
			}
			state, paused, err := readWatchPause(s.Root)
			if err != nil {
				return fmt.Errorf("reading watch pause state: %w", err)
			}
			if !paused {
				fmt.Fprintln(cmd.OutOrStdout(), "watch is active")
				return nil
			}
			fmt.Fprintln(cmd.OutOrStdout(), watchPauseSummary(state))
			return nil
		},
	}
}
