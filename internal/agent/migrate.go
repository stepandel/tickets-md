package agent

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// MigrateFlat moves any pre-per-run agent files into the new
// per-ticket-directory layout. Idempotent: a second call is a no-op.
//
// Old layout:  .tickets/.agents/TIC-001.yml + .log + .exit
// New layout:  .tickets/.agents/TIC-001/001-<stage>.yml + .log + .exit
//
// Files with no recoverable stage are skipped (logged, left in place).
func MigrateFlat(root string) error {
	dir := Dir(root)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".yml") {
			continue
		}
		if strings.HasSuffix(name, ".tmp") {
			continue
		}

		flatPath := filepath.Join(dir, name)
		ticketID := strings.TrimSuffix(name, ".yml")

		data, err := os.ReadFile(flatPath)
		if err != nil {
			log.Printf("agent migrate: read %s: %v", flatPath, err)
			continue
		}
		var old AgentStatus
		if err := yaml.Unmarshal(data, &old); err != nil {
			log.Printf("agent migrate: parse %s: %v", flatPath, err)
			continue
		}
		if old.Stage == "" {
			log.Printf("agent migrate: %s has no stage, skipping", flatPath)
			continue
		}
		if old.TicketID == "" {
			old.TicketID = ticketID
		}

		old.Seq = 1
		old.Attempt = 1
		old.RunID = FormatRunID(old.Seq, old.Stage)

		newDir := TicketDir(root, ticketID)
		if err := os.MkdirAll(newDir, 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", newDir, err)
		}

		newLog := LogPath(root, ticketID, old.RunID)
		newExit := ExitPath(root, ticketID, old.RunID)

		oldLog := filepath.Join(dir, ticketID+".log")
		oldExit := filepath.Join(dir, ticketID+".exit")

		if err := moveOrCopy(oldLog, newLog); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Printf("agent migrate: move log %s: %v", oldLog, err)
		}
		if err := moveOrCopy(oldExit, newExit); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Printf("agent migrate: move exit %s: %v", oldExit, err)
		}

		old.LogFile = newLog
		old.ExitFile = newExit

		out, err := yaml.Marshal(old)
		if err != nil {
			log.Printf("agent migrate: marshal %s: %v", ticketID, err)
			continue
		}
		newYml := runPath(root, ticketID, old.RunID)
		if err := os.WriteFile(newYml, out, 0o644); err != nil {
			log.Printf("agent migrate: write %s: %v", newYml, err)
			continue
		}
		if err := os.Remove(flatPath); err != nil {
			log.Printf("agent migrate: remove %s: %v", flatPath, err)
		}
		log.Printf("agent migrate: %s -> %s/%s.yml", ticketID, ticketID, old.RunID)
	}
	return nil
}

// moveOrCopy renames src to dst, falling back to copy+remove if rename
// fails (e.g. crossing filesystems).
func moveOrCopy(src, dst string) error {
	if _, err := os.Stat(src); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Remove(src)
}
