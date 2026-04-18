package cli

import (
	"reflect"
	"testing"

	"github.com/stepandel/tickets-md/internal/agent"
	"github.com/stepandel/tickets-md/internal/config"
	"github.com/stepandel/tickets-md/internal/ticket"
)

func TestStartSetLabelsUsesConfiguredOrderAndMarkers(t *testing.T) {
	s := newCleanupStoreWithConfig(t, config.Config{
		Prefix:        "TIC",
		ProjectPrefix: "PRJ",
		Stages:        []string{"backlog", "done"},
		Labels: map[string]config.LabelConfig{
			"Customer": {Color: "#dc2626", Order: intPtr(0)},
			"Backend":  {Color: "#0f766e"},
		},
	})
	m := &boardModel{
		store:         s,
		stages:        s.Config.Stages,
		columns:       [][]ticket.Ticket{{{ID: "TIC-001", Labels: []string{"backend", "Legacy"}}}},
		agentStatuses: map[string]agent.AgentStatus{},
		scrollOff:     []int{0},
		visibleCards:  []int{1},
	}

	m.startSetLabels()

	if m.overlayKind != "labels" {
		t.Fatalf("overlayKind = %q, want labels", m.overlayKind)
	}
	picker, ok := m.overlay.(*pickerOverlay)
	if !ok {
		t.Fatalf("overlay = %T, want *pickerOverlay", m.overlay)
	}

	var gotLabels []string
	var gotKeys []string
	for _, item := range picker.items {
		gotLabels = append(gotLabels, item.label)
		gotKeys = append(gotKeys, item.key)
	}

	wantLabels := []string{"Customer", "Backend", "Legacy", "+ create label..."}
	if !reflect.DeepEqual(gotLabels, wantLabels) {
		t.Fatalf("picker labels = %#v, want %#v", gotLabels, wantLabels)
	}
	wantKeys := []string{"", "(assigned)", "(unconfigured)", ""}
	if !reflect.DeepEqual(gotKeys, wantKeys) {
		t.Fatalf("picker keys = %#v, want %#v", gotKeys, wantKeys)
	}
}

func TestApplyLabelChoiceTogglesAssignedAndUnconfiguredLabels(t *testing.T) {
	s := newCleanupStoreWithConfig(t, config.Config{
		Prefix:        "TIC",
		ProjectPrefix: "PRJ",
		Stages:        []string{"backlog", "done"},
		Labels: map[string]config.LabelConfig{
			"Backend": {Color: "#0f766e"},
		},
	})
	tk, err := s.Create("Ticket")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	tk.Labels = []string{"Legacy"}
	if err := s.Save(tk); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := s.Get(tk.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	m := &boardModel{
		store:         s,
		stages:        s.Config.Stages,
		columns:       [][]ticket.Ticket{{got}},
		agentStatuses: map[string]agent.AgentStatus{},
		scrollOff:     []int{0},
		visibleCards:  []int{1},
	}

	m.applyLabelChoice(&pickerOverlay{choice: &pickerItem{value: "Backend"}})
	got, err = s.Get(tk.ID)
	if err != nil {
		t.Fatalf("Get after add: %v", err)
	}
	if !reflect.DeepEqual(got.Labels, []string{"Legacy", "Backend"}) {
		t.Fatalf("Labels after add = %#v", got.Labels)
	}
	m.columns[0][0] = got

	m.applyLabelChoice(&pickerOverlay{choice: &pickerItem{value: "Legacy"}})
	got, err = s.Get(tk.ID)
	if err != nil {
		t.Fatalf("Get after remove: %v", err)
	}
	if !reflect.DeepEqual(got.Labels, []string{"Backend"}) {
		t.Fatalf("Labels after remove = %#v", got.Labels)
	}
}

func TestApplyCreateLabelCreatesConfigAndAssignsLabel(t *testing.T) {
	s := newCleanupStoreWithConfig(t, config.Config{
		Prefix:        "TIC",
		ProjectPrefix: "PRJ",
		Stages:        []string{"backlog", "done"},
	})
	tk, err := s.Create("Ticket")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := s.Get(tk.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	m := &boardModel{
		store:         s,
		stages:        s.Config.Stages,
		columns:       [][]ticket.Ticket{{got}},
		agentStatuses: map[string]agent.AgentStatus{},
		scrollOff:     []int{0},
		visibleCards:  []int{1},
	}

	m.applyCreateLabel(&textInputOverlay{value: "Backend"})

	reopened, err := ticket.Open(s.Root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	labelCfg, ok := reopened.Config.Labels["Backend"]
	if !ok {
		t.Fatal("expected Backend label in config")
	}
	if labelCfg.Color != defaultNewLabelColor {
		t.Fatalf("color = %q, want %q", labelCfg.Color, defaultNewLabelColor)
	}
	got, err = reopened.Get(tk.ID)
	if err != nil {
		t.Fatalf("Get reopened: %v", err)
	}
	if !reflect.DeepEqual(got.Labels, []string{"Backend"}) {
		t.Fatalf("Labels = %#v, want Backend", got.Labels)
	}
}

func TestApplyCreateLabelReusesExistingConfiguredKey(t *testing.T) {
	s := newCleanupStoreWithConfig(t, config.Config{
		Prefix:        "TIC",
		ProjectPrefix: "PRJ",
		Stages:        []string{"backlog", "done"},
		Labels: map[string]config.LabelConfig{
			"Backend": {Color: "#0f766e"},
		},
	})
	tk, err := s.Create("Ticket")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := s.Get(tk.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	m := &boardModel{
		store:         s,
		stages:        s.Config.Stages,
		columns:       [][]ticket.Ticket{{got}},
		agentStatuses: map[string]agent.AgentStatus{},
		scrollOff:     []int{0},
		visibleCards:  []int{1},
	}

	m.applyCreateLabel(&textInputOverlay{value: " backend "})

	got, err = s.Get(tk.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !reflect.DeepEqual(got.Labels, []string{"Backend"}) {
		t.Fatalf("Labels = %#v, want Backend", got.Labels)
	}
	if len(s.Config.Labels) != 1 {
		t.Fatalf("labels config = %#v, want no duplicate", s.Config.Labels)
	}
}
