// Package worktree manages git worktrees for per-ticket agent
// isolation. Each ticket gets its own checkout and branch so
// multiple agents can work concurrently without conflicts.
package worktree

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	// Dir is the directory under the project root where worktrees live.
	Dir = ".worktrees"
	// BranchPrefix namespaces worktree branches to avoid collisions.
	BranchPrefix = "tickets/"
)

// Info describes an existing worktree.
type Info struct {
	Path   string // absolute path to the worktree directory
	Branch string // branch name (e.g. tickets/TIC-001)
}

// Create creates a new git worktree for a ticket. It branches from
// baseBranch (or HEAD if empty) and returns the absolute worktree path.
//
//	git worktree add .worktrees/TIC-001 -b tickets/TIC-001 [baseBranch]
func Create(root, ticketID, baseBranch string) (string, error) {
	wtDir := filepath.Join(root, Dir, ticketID)
	branch := BranchPrefix + ticketID

	// If the worktree already exists, just return its path.
	if info, err := os.Stat(wtDir); err == nil && info.IsDir() {
		return wtDir, nil
	}

	// Ensure parent directory exists.
	if err := os.MkdirAll(filepath.Join(root, Dir), 0o755); err != nil {
		return "", err
	}

	args := []string{"worktree", "add", wtDir, "-b", branch}
	if baseBranch != "" {
		args = append(args, baseBranch)
	}

	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		// If branch already exists (from a previous run), try without -b.
		if strings.Contains(string(out), "already exists") {
			args = []string{"worktree", "add", wtDir, branch}
			cmd = exec.Command("git", args...)
			cmd.Dir = root
			out, err = cmd.CombinedOutput()
			if err != nil {
				return "", fmt.Errorf("git worktree add: %s", strings.TrimSpace(string(out)))
			}
			return wtDir, nil
		}
		return "", fmt.Errorf("git worktree add: %s", strings.TrimSpace(string(out)))
	}
	return wtDir, nil
}

// DeleteBranch deletes the ticket's branch (tickets/<id>). It is a
// no-op if the branch does not exist.
func DeleteBranch(root, ticketID string) error {
	branch := BranchPrefix + ticketID
	cmd := exec.Command("git", "branch", "-D", branch)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		// Not an error if the branch simply doesn't exist.
		if strings.Contains(msg, "not found") {
			return nil
		}
		return fmt.Errorf("git branch -D %s: %s", branch, msg)
	}
	return nil
}

// Remove removes a worktree directory and prunes git's record of it.
func Remove(root, ticketID string) error {
	wtDir := filepath.Join(root, Dir, ticketID)
	cmd := exec.Command("git", "worktree", "remove", "--force", wtDir)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git worktree remove: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// List returns all worktrees under .worktrees/.
func List(root string) ([]Info, error) {
	dir := filepath.Join(root, Dir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var infos []Info
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		wtPath := filepath.Join(dir, e.Name())
		branch := branchForWorktree(root, wtPath)
		infos = append(infos, Info{
			Path:   wtPath,
			Branch: branch,
		})
	}
	return infos, nil
}

// branchForWorktree reads the current branch of a worktree.
func branchForWorktree(root, wtPath string) string {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = wtPath
	out, err := cmd.Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

// EnsureGitignored adds .worktrees to .gitignore if not already present.
func EnsureGitignored(root string) error {
	gitignore := filepath.Join(root, ".gitignore")
	data, err := os.ReadFile(gitignore)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	entry := ".worktrees"
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == entry {
			return nil // already present
		}
	}
	f, err := os.OpenFile(gitignore, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	// Add a newline before the entry if file doesn't end with one.
	if len(data) > 0 && data[len(data)-1] != '\n' {
		f.WriteString("\n")
	}
	_, err = f.WriteString(entry + "\n")
	return err
}
