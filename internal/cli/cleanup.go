package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/stepandel/tickets-md/internal/agent"
	"github.com/stepandel/tickets-md/internal/config"
	"github.com/stepandel/tickets-md/internal/ticket"
	"github.com/stepandel/tickets-md/internal/worktree"
)

type cleanupOptions struct {
	orphansOnly bool
	stagesOnly  bool
}

type cleanupAction struct {
	Description string
	Do          func() error
}

func newCleanupCmd() *cobra.Command {
	var dryRun bool
	var opts cleanupOptions
	cmd := &cobra.Command{
		Use:   "cleanup",
		Short: "Clean orphaned agent data, worktrees, and configured archive-stage artifacts",
		Long: `cleanup scans the store for orphaned agent data, orphaned git
worktrees, and optional configured-stage artifacts. It force-deletes
tickets/<id> branches when asked. Run it while tickets watch is idle
to avoid racing a live agent spawn.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.orphansOnly && opts.stagesOnly {
				return fmt.Errorf("--orphans-only and --stages-only are mutually exclusive")
			}
			s, err := openStore()
			if err != nil {
				return err
			}
			actions, warnings, err := collectCleanupActions(s, opts)
			if err != nil {
				return err
			}
			for _, warning := range warnings {
				fmt.Fprintf(cmd.OutOrStdout(), "[cleanup] %s\n", warning)
			}
			performed, failures := executeCleanupActions(cmd.OutOrStdout(), actions, dryRun)
			if failures > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "\n%d action(s), %d performed, %d failed\n", len(actions), performed, failures)
				return nil
			}
			if dryRun {
				fmt.Fprintf(cmd.OutOrStdout(), "\n%d action(s), %d performed (dry run)\n", len(actions), performed)
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "\n%d action(s), %d performed\n", len(actions), performed)
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "report actions without performing them")
	cmd.Flags().BoolVar(&opts.orphansOnly, "orphans-only", false, "only remove orphan agent dirs, worktrees, and branches")
	cmd.Flags().BoolVar(&opts.stagesOnly, "stages-only", false, "only clean artifacts for configured cleanup stages")
	return cmd
}

func collectCleanupActions(s *ticket.Store, opts cleanupOptions) ([]cleanupAction, []string, error) {
	var actions []cleanupAction
	var warnings []string

	branchSet, err := ticketBranchSet(s.Root)
	if err != nil {
		return nil, nil, err
	}

	if !opts.stagesOnly {
		orphanAgents, err := findOrphanAgentIDs(s)
		if err != nil {
			return nil, nil, err
		}
		for _, id := range orphanAgents {
			ticketID := id
			actions = append(actions, cleanupAction{
				Description: fmt.Sprintf("remove agent data for orphan %s", ticketID),
				Do: func() error {
					return agent.RemoveTicket(s.Root, ticketID)
				},
			})
		}

		orphanWorktrees, err := findOrphanWorktreeIDs(s)
		if err != nil {
			return nil, nil, err
		}
		for _, id := range orphanWorktrees {
			ticketID := id
			actions = append(actions, cleanupAction{
				Description: fmt.Sprintf("remove orphan worktree %s", ticketID),
				Do: func() error {
					return worktree.Remove(s.Root, ticketID)
				},
			})
			if _, ok := branchSet[worktree.BranchPrefix+ticketID]; ok {
				actions = append(actions, cleanupAction{
					Description: fmt.Sprintf("delete orphan branch %s%s", worktree.BranchPrefix, ticketID),
					Do: func() error {
						return worktree.DeleteBranch(s.Root, ticketID)
					},
				})
			}
		}

		orphanBranches, err := findOrphanBranchIDs(s)
		if err != nil {
			return nil, nil, err
		}
		for _, branch := range orphanBranches {
			ticketID := strings.TrimPrefix(branch, worktree.BranchPrefix)
			actions = append(actions, cleanupAction{
				Description: fmt.Sprintf("delete orphan branch %s", branch),
				Do: func() error {
					return worktree.DeleteBranch(s.Root, ticketID)
				},
			})
		}
	}

	if !opts.orphansOnly && s.Config.Cleanup != nil {
		stageActions, stageWarnings, err := collectConfiguredStageActions(s, s.Config.Cleanup, branchSet)
		if err != nil {
			return nil, nil, err
		}
		actions = append(actions, stageActions...)
		warnings = append(warnings, stageWarnings...)
	}

	return actions, warnings, nil
}

func executeCleanupActions(out io.Writer, actions []cleanupAction, dryRun bool) (performed int, failures int) {
	for _, action := range actions {
		if dryRun {
			fmt.Fprintf(out, "[cleanup] %s (dry run)\n", action.Description)
			continue
		}
		if err := action.Do(); err != nil {
			failures++
			fmt.Fprintf(out, "[cleanup] %s (failed: %v)\n", action.Description, err)
			continue
		}
		performed++
		fmt.Fprintf(out, "[cleanup] %s (performed)\n", action.Description)
	}
	return performed, failures
}

func findOrphanAgentIDs(s *ticket.Store) ([]string, error) {
	entries, err := os.ReadDir(agent.Dir(s.Root))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var ids []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		id := entry.Name()
		if _, err := s.Get(id); err == nil {
			continue
		} else if !errors.Is(err, ticket.ErrNotFound) {
			return nil, err
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}

func findOrphanWorktreeIDs(s *ticket.Store) ([]string, error) {
	infos, err := worktree.List(s.Root)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, info := range infos {
		id := filepath.Base(info.Path)
		if _, err := s.Get(id); err == nil {
			continue
		} else if !errors.Is(err, ticket.ErrNotFound) {
			return nil, err
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}

func findOrphanBranchIDs(s *ticket.Store) ([]string, error) {
	branches, err := ticketBranches(s.Root)
	if err != nil {
		return nil, err
	}
	worktrees, err := worktree.List(s.Root)
	if err != nil {
		return nil, err
	}
	worktreeIDs := make(map[string]struct{}, len(worktrees))
	for _, info := range worktrees {
		worktreeIDs[filepath.Base(info.Path)] = struct{}{}
	}
	var orphans []string
	for _, branch := range branches {
		id := strings.TrimPrefix(branch, worktree.BranchPrefix)
		if _, ok := worktreeIDs[id]; ok {
			continue
		}
		if _, err := s.Get(id); err == nil {
			continue
		} else if !errors.Is(err, ticket.ErrNotFound) {
			return nil, err
		}
		orphans = append(orphans, branch)
	}
	sort.Strings(orphans)
	return orphans, nil
}

func collectConfiguredStageActions(s *ticket.Store, cfg *config.CleanupConfig, branchSet map[string]struct{}) ([]cleanupAction, []string, error) {
	var actions []cleanupAction
	var warnings []string
	for _, stageCfg := range cfg.Stages {
		tickets, err := s.List(stageCfg.Name)
		if err != nil {
			return nil, nil, err
		}
		for _, tk := range tickets {
			latest, latestErr := agent.Latest(s.Root, tk.ID)
			activeRun := latestErr == nil && !latest.Status.IsTerminal()
			if activeRun {
				warnings = append(warnings, fmt.Sprintf("skip %s in %s: latest run %s is %s", tk.ID, stageCfg.Name, latest.RunID, latest.Status))
				continue
			}
			if stageCfg.AgentData && agentDataExists(s.Root, tk.ID) {
				ticketID := tk.ID
				stageName := stageCfg.Name
				actions = append(actions, cleanupAction{
					Description: fmt.Sprintf("remove agent data for %s in %s", ticketID, stageName),
					Do: func() error {
						return agent.RemoveTicket(s.Root, ticketID)
					},
				})
			}
			if stageCfg.Worktree && worktreeExists(s.Root, tk.ID) {
				ticketID := tk.ID
				stageName := stageCfg.Name
				actions = append(actions, cleanupAction{
					Description: fmt.Sprintf("remove worktree for %s in %s", ticketID, stageName),
					Do: func() error {
						return worktree.Remove(s.Root, ticketID)
					},
				})
			}
			if stageCfg.Branch {
				branch := worktree.BranchPrefix + tk.ID
				if _, ok := branchSet[branch]; ok {
					ticketID := tk.ID
					stageName := stageCfg.Name
					actions = append(actions, cleanupAction{
						Description: fmt.Sprintf("delete branch %s for %s in %s", branch, ticketID, stageName),
						Do: func() error {
							return worktree.DeleteBranch(s.Root, ticketID)
						},
					})
				}
			}
		}
	}
	return actions, warnings, nil
}

func agentDataExists(root, ticketID string) bool {
	info, err := os.Stat(agent.TicketDir(root, ticketID))
	return err == nil && info.IsDir()
}

func worktreeExists(root, ticketID string) bool {
	info, err := os.Stat(filepath.Join(root, worktree.Dir, ticketID))
	return err == nil && info.IsDir()
}

func ticketBranchSet(root string) (map[string]struct{}, error) {
	branches, err := ticketBranches(root)
	if err != nil {
		return nil, err
	}
	out := make(map[string]struct{}, len(branches))
	for _, branch := range branches {
		out[branch] = struct{}{}
	}
	return out, nil
}

func ticketBranches(root string) ([]string, error) {
	cmd := exec.Command("git", "for-each-ref", "--format=%(refname:short)", "refs/heads/"+worktree.BranchPrefix)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("list cleanup branches: %s", strings.TrimSpace(string(out)))
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil, nil
	}
	var branches []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		branches = append(branches, line)
	}
	sort.Strings(branches)
	return branches, nil
}
