package cli

import (
	"slices"
	"testing"

	"github.com/stepandel/tickets-md/internal/config"
)

func TestVisibleStages(t *testing.T) {
	cfg := config.Config{
		Prefix:        "TIC",
		ProjectPrefix: "PRJ",
		Stages:        []string{"backlog", "done", "archive"},
		ArchiveStage:  "archive",
	}

	got := visibleStages(cfg, false)
	if !slices.Equal(got, []string{"backlog", "done"}) {
		t.Fatalf("visibleStages(false) = %v, want [backlog done]", got)
	}

	got = visibleStages(cfg, true)
	if !slices.Equal(got, cfg.Stages) {
		t.Fatalf("visibleStages(true) = %v, want %v", got, cfg.Stages)
	}
}

func TestNewBoardModelHidesArchiveStageByDefault(t *testing.T) {
	s := newArchiveStore(t)

	m, err := newBoardModel(s, "", false)
	if err != nil {
		t.Fatalf("newBoardModel(showArchived=false): %v", err)
	}
	if len(m.stages) != len(s.Config.Stages)-1 {
		t.Fatalf("len(m.stages) = %d, want %d", len(m.stages), len(s.Config.Stages)-1)
	}

	m, err = newBoardModel(s, "", true)
	if err != nil {
		t.Fatalf("newBoardModel(showArchived=true): %v", err)
	}
	if len(m.stages) != len(s.Config.Stages) {
		t.Fatalf("len(m.stages) = %d, want %d", len(m.stages), len(s.Config.Stages))
	}
}
