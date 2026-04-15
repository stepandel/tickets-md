package agent

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/stepandel/tickets-md/internal/config"
)

func CronOwnerID(name string) string {
	return cronNamespace + "/" + name
}

func IsCronOwner(ownerID string) bool {
	return strings.HasPrefix(ownerID, cronNamespace+"/")
}

func CronName(ownerID string) (string, bool) {
	if !IsCronOwner(ownerID) {
		return "", false
	}
	name := strings.TrimPrefix(ownerID, cronNamespace+"/")
	if name == "" || strings.Contains(name, "/") {
		return "", false
	}
	return name, true
}

func CronRootDir(root string) string {
	return filepath.Join(Dir(root), cronNamespace)
}

func CronDir(root, name string) string {
	return TicketDir(root, CronOwnerID(name))
}

func CronRunsDir(root, name string) string {
	return RunsDir(root, CronOwnerID(name))
}

func CronLogPath(root, name, runID string) string {
	return LogPath(root, CronOwnerID(name), runID)
}

func CronReadRun(root, name, runID string) (AgentStatus, error) {
	return ReadRun(root, CronOwnerID(name), runID)
}

func CronLatest(root, name string) (AgentStatus, error) {
	return Latest(root, CronOwnerID(name))
}

func CronHistory(root, name string) ([]AgentStatus, error) {
	return History(root, CronOwnerID(name))
}

func CronNextRun(root, name string) (runID string, seq, attempt int, err error) {
	if err := config.ValidateCronName(name); err != nil {
		return "", 0, 0, err
	}
	return NextRun(root, CronOwnerID(name), "cron")
}

func ListCronRuns(root string) ([]AgentStatus, error) {
	entries, err := os.ReadDir(CronRootDir(root))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var statuses []AgentStatus
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		as, err := CronLatest(root, e.Name())
		if err != nil {
			continue
		}
		statuses = append(statuses, as)
	}
	sort.Slice(statuses, func(i, j int) bool {
		return statuses[i].SpawnedAt.Before(statuses[j].SpawnedAt)
	})
	return statuses, nil
}

func ListAllCronRuns(root string) ([]AgentStatus, error) {
	entries, err := os.ReadDir(CronRootDir(root))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var all []AgentStatus
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		runs, err := CronHistory(root, e.Name())
		if err != nil {
			continue
		}
		all = append(all, runs...)
	}
	sort.Slice(all, func(i, j int) bool {
		return all[i].SpawnedAt.Before(all[j].SpawnedAt)
	})
	return all, nil
}

func RemoveCron(root, name string) error {
	return RemoveTicket(root, CronOwnerID(name))
}
