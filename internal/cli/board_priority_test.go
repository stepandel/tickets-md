package cli

import (
	"image/color"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/stepandel/tickets-md/internal/config"
)

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
