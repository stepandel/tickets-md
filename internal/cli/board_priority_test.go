package cli

import (
	"image/color"
	"reflect"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/stepandel/tickets-md/internal/agent"
	"github.com/stepandel/tickets-md/internal/config"
	"github.com/stepandel/tickets-md/internal/ticket"
)

func intPtr(v int) *int {
	return &v
}

func TestPriorityStyle(t *testing.T) {
	tests := []struct {
		name      string
		cfg       config.Config
		value     string
		wantColor color.Color
		wantBold  bool
	}{
		{
			name:      "default critical",
			cfg:       config.Config{},
			value:     "critical",
			wantColor: lipgloss.Color("#FF5F5F"),
			wantBold:  true,
		},
		{
			name:      "default medium alias",
			cfg:       config.Config{},
			value:     "med",
			wantColor: lipgloss.Color("#FFD700"),
			wantBold:  false,
		},
		{
			name: "configured custom priority",
			cfg: config.Config{
				Priorities: map[string]config.PriorityConfig{
					"P0": {Color: "#112233", Bold: true},
				},
			},
			value:     " p0 ",
			wantColor: lipgloss.Color("#112233"),
			wantBold:  true,
		},
		{
			name: "configured unknown falls back to gold",
			cfg: config.Config{
				Priorities: map[string]config.PriorityConfig{
					"P0": {Color: "#112233", Bold: true},
				},
			},
			value:     "critical",
			wantColor: lipgloss.Color("#FFD700"),
			wantBold:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			style := priorityStyle(tt.cfg, tt.value)
			if got := style.GetForeground(); got != tt.wantColor {
				t.Fatalf("GetForeground() = %#v, want %#v", got, tt.wantColor)
			}
			if got := style.GetBold(); got != tt.wantBold {
				t.Fatalf("GetBold() = %v, want %v", got, tt.wantBold)
			}
		})
	}
}

func TestStartSetPriorityUsesConfiguredOrder(t *testing.T) {
	s := newCleanupStoreWithConfig(t, config.Config{
		Prefix:        "TIC",
		ProjectPrefix: "PRJ",
		Stages:        []string{"backlog", "done"},
		Priorities: map[string]config.PriorityConfig{
			"Medium":  {Color: "#333333", Order: intPtr(10)},
			"P0":      {Color: "#111111", Bold: true, Order: intPtr(0)},
			"z-last":  {Color: "#999999"},
			"A first": {Color: "#aaaaaa"},
		},
	})
	m := &boardModel{
		store:         s,
		stages:        s.Config.Stages,
		columns:       [][]ticket.Ticket{{{ID: "TIC-001", Priority: "p0"}}},
		agentStatuses: map[string]agent.AgentStatus{},
		scrollOff:     []int{0},
		visibleCards:  []int{1},
	}

	m.startSetPriority()

	if m.overlayKind != "priority" {
		t.Fatalf("overlayKind = %q, want priority", m.overlayKind)
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

	wantLabels := []string{"P0", "Medium", "A first", "z-last", "none"}
	if !reflect.DeepEqual(gotLabels, wantLabels) {
		t.Fatalf("picker labels = %#v, want %#v", gotLabels, wantLabels)
	}
	wantKeys := []string{"(current)", "", "", "", ""}
	if !reflect.DeepEqual(gotKeys, wantKeys) {
		t.Fatalf("picker keys = %#v, want %#v", gotKeys, wantKeys)
	}
}

func TestStartSetPriorityDefaultsWhenPrioritiesAbsent(t *testing.T) {
	s := newCleanupStore(t)
	m := &boardModel{
		store:         s,
		stages:        s.Config.Stages,
		columns:       [][]ticket.Ticket{{{ID: "TIC-002"}}},
		agentStatuses: map[string]agent.AgentStatus{},
		scrollOff:     []int{0},
		visibleCards:  []int{1},
	}

	m.startSetPriority()

	picker, ok := m.overlay.(*pickerOverlay)
	if !ok {
		t.Fatalf("overlay = %T, want *pickerOverlay", m.overlay)
	}
	var got []string
	for _, item := range picker.items {
		got = append(got, item.label)
	}
	want := []string{"critical", "high", "medium", "low", "none"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("picker labels = %#v, want %#v", got, want)
	}
}
