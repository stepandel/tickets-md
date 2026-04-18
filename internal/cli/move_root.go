package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/stepandel/tickets-md/internal/ticket"
)

func openStoreAt(root string) (*ticket.Store, error) {
	s, err := ticket.Open(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("no ticket store here — run `tickets init` first")
		}
		return nil, err
	}
	return s, nil
}

func resolveStoreRoot(root string) (string, bool, error) {
	if globalFlags.rootExplicit {
		return root, false, nil
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", false, err
	}

	gitDir, err := resolveGitPath(absRoot, "rev-parse", "--git-dir")
	if err != nil {
		return absRoot, false, nil
	}
	commonDir, err := resolveGitPath(absRoot, "rev-parse", "--git-common-dir")
	if err != nil {
		return absRoot, false, nil
	}
	if gitDir == commonDir {
		return absRoot, false, nil
	}

	mainRoot := filepath.Dir(commonDir)
	if _, err := os.Stat(filepath.Join(mainRoot, ".tickets", "config.yml")); err != nil {
		return absRoot, false, nil
	}
	return mainRoot, true, nil
}

func resolveGitPath(root string, args ...string) (string, error) {
	out, err := runGit(root, args...)
	if err != nil {
		return "", err
	}
	path := strings.TrimSpace(out)
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	return filepath.Clean(path), nil
}
