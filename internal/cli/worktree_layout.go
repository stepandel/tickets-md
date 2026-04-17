package cli

import (
	"github.com/stepandel/tickets-md/internal/config"
	"github.com/stepandel/tickets-md/internal/worktree"
)

func worktreeLayout(cfg config.Config) worktree.Layout {
	return worktree.Layout{
		Dir:          cfg.WorktreeDir(),
		BranchPrefix: cfg.WorktreeBranchPrefix(),
	}
}
