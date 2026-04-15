package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/stepandel/tickets-md/internal/updatecheck"
	"github.com/stepandel/tickets-md/internal/userconfig"
)

const updateCheckDisableEnv = "TICKETS_UPDATE_CHECK_DISABLE"

func maybeNagForUpdate(cmd *cobra.Command) error {
	if shouldSkipUpdateCheck(cmd) {
		return nil
	}

	cfg, _, err := userconfig.Load()
	if err != nil {
		return nil
	}

	cache := updatecheck.Cache{
		LastCheckedAt: cfg.UpdateCheck.LastCheckedAt,
		LatestVersion: cfg.UpdateCheck.LatestVersion,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	nextCache, nag, err := updatecheck.Check(ctx, version, time.Now().UTC(), cache)
	if err != nil {
		return nil
	}
	if nag != "" {
		fmt.Fprintln(cmd.ErrOrStderr(), nag)
	}
	if nextCache == cache {
		return nil
	}

	cfg.UpdateCheck = userconfig.UpdateCheck{
		LastCheckedAt: nextCache.LastCheckedAt,
		LatestVersion: nextCache.LatestVersion,
	}
	if err := userconfig.Save(cfg); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "tickets: warning: could not save update-check cache: %v\n", err)
	}
	return nil
}

func shouldSkipUpdateCheck(cmd *cobra.Command) bool {
	if os.Getenv("TICKETS_NO_UPDATE_CHECK") == "1" || os.Getenv(updateCheckDisableEnv) == "1" {
		return true
	}
	if strings.HasSuffix(os.Args[0], ".test") {
		return true
	}
	if version == "dev" || version == "(devel)" {
		return true
	}
	if !isTerminal(os.Stderr) {
		return true
	}
	if cmd.Name() == "completion" {
		return true
	}
	if cmd.Root() == cmd {
		if versionFlag, err := cmd.Flags().GetBool("version"); err == nil && versionFlag {
			return true
		}
		if len(os.Args) == 2 && os.Args[1] == "--version" {
			return true
		}
	}
	return false
}

func init() {
	if os.Getenv(updateCheckDisableEnv) == "" && strings.HasSuffix(filepath.Base(os.Args[0]), ".test") {
		os.Setenv(updateCheckDisableEnv, "1")
	}
}
