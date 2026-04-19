package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/stepandel/tickets-md/internal/config"
)

type watcherLockInfo struct {
	PID       int       `json:"pid"`
	Hostname  string    `json:"hostname"`
	StartedAt time.Time `json:"started_at"`
}

type watcherLock struct {
	file *os.File
	path string
}

type duplicateWatcherError struct {
	path string
	info watcherLockInfo
}

func (e *duplicateWatcherError) Error() string {
	var parts []string
	if e.info.PID > 0 {
		parts = append(parts, fmt.Sprintf("pid %d", e.info.PID))
	}
	if e.info.Hostname != "" {
		parts = append(parts, "on "+e.info.Hostname)
	}
	if !e.info.StartedAt.IsZero() {
		parts = append(parts, "started "+e.info.StartedAt.Format(time.RFC3339))
	}

	owner := "owner details unavailable"
	if len(parts) > 0 {
		owner = strings.Join(parts, ", ")
	}

	msg := fmt.Sprintf("tickets watch: another watcher is already running for this repo (%s). Stop it first", owner)
	if e.info.PID > 0 {
		msg += fmt.Sprintf(" (for example: kill %d)", e.info.PID)
	}
	msg += fmt.Sprintf(", or remove %s if you are sure no watcher is running", e.path)
	return msg
}

func watcherLockPath(root string) string {
	return filepath.Join(root, config.ConfigDir, ".watch.lock")
}

func acquireWatcherLock(root string) (*watcherLock, error) {
	path := watcherLockPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		info, _ := readWatcherLockInfo(f)
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, &duplicateWatcherError{path: path, info: info}
		}
		return nil, err
	}

	info := watcherLockInfo{
		PID:       os.Getpid(),
		StartedAt: time.Now().UTC().Truncate(time.Second),
	}
	if hostname, err := os.Hostname(); err == nil {
		info.Hostname = hostname
	}
	if err := writeWatcherLockInfo(f, info); err != nil {
		f.Close()
		return nil, err
	}

	return &watcherLock{file: f, path: path}, nil
}

func readWatcherLockInfo(f *os.File) (watcherLockInfo, error) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return watcherLockInfo{}, err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return watcherLockInfo{}, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return watcherLockInfo{}, nil
	}

	var info watcherLockInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return watcherLockInfo{}, err
	}
	return info, nil
}

func writeWatcherLockInfo(f *os.File, info watcherLockInfo) error {
	data, err := json.Marshal(info)
	if err != nil {
		return err
	}
	if err := f.Truncate(0); err != nil {
		return err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		return err
	}
	return f.Sync()
}

func (l *watcherLock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}

	unlockErr := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	closeErr := l.file.Close()
	l.file = nil

	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}
