package cli

import (
	"strings"
	"testing"

	"github.com/stepandel/tickets-md/internal/config"
	"github.com/stepandel/tickets-md/internal/ticket"
)

func newCronCLITestStore(t *testing.T) *ticket.Store {
	t.Helper()
	s := newCLITestStore(t)
	s.Config.CronAgents = []config.CronAgentConfig{
		{
			Name:     "groomer",
			Schedule: "@every 5m",
			Command:  "codex",
			Args:     []string{"run", "--json"},
			Prompt:   "groom the board",
		},
	}
	if err := config.Save(s.Root, s.Config); err != nil {
		t.Fatalf("Save config: %v", err)
	}
	reloaded, err := ticket.Open(s.Root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return reloaded
}

func loadCronConfig(t *testing.T, root string) config.CronAgentConfig {
	t.Helper()
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatalf("Load config: %v", err)
	}
	if len(cfg.CronAgents) != 1 {
		t.Fatalf("CronAgents = %#v, want one entry", cfg.CronAgents)
	}
	return cfg.CronAgents[0]
}

func TestCronsEnableClearsEnabledPointerAndIsIdempotent(t *testing.T) {
	s := newCronCLITestStore(t)
	disabled := false
	s.Config.CronAgents[0].Enabled = &disabled
	if err := config.Save(s.Root, s.Config); err != nil {
		t.Fatalf("Save disabled config: %v", err)
	}

	globalFlags.root = s.Root
	cmd := newCronsEnableCmd()
	cmd.SetArgs([]string{"groomer"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute enable: %v", err)
	}
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute enable second time: %v", err)
	}

	cron := loadCronConfig(t, s.Root)
	if cron.Enabled != nil {
		t.Fatalf("Enabled = %#v, want nil", cron.Enabled)
	}
	if !cron.IsEnabled() {
		t.Fatalf("cron should be enabled: %#v", cron)
	}
}

func TestCronsDisableSetsFalseAndIsIdempotent(t *testing.T) {
	s := newCronCLITestStore(t)

	globalFlags.root = s.Root
	cmd := newCronsDisableCmd()
	cmd.SetArgs([]string{"groomer"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute disable: %v", err)
	}
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute disable second time: %v", err)
	}

	cron := loadCronConfig(t, s.Root)
	if cron.Enabled == nil {
		t.Fatalf("Enabled = nil, want false")
	}
	if *cron.Enabled {
		t.Fatalf("Enabled = true, want false")
	}
	if cron.IsEnabled() {
		t.Fatalf("cron should be disabled: %#v", cron)
	}
}

func TestCronsSetUpdatesSupportedFields(t *testing.T) {
	s := newCronCLITestStore(t)

	tests := []struct {
		args  []string
		check func(t *testing.T, cron config.CronAgentConfig)
	}{
		{
			args: []string{"groomer", "schedule", "@every", "10m"},
			check: func(t *testing.T, cron config.CronAgentConfig) {
				t.Helper()
				if cron.Schedule != "@every 10m" {
					t.Fatalf("Schedule = %q, want @every 10m", cron.Schedule)
				}
			},
		},
		{
			args: []string{"groomer", "command", "claude", "code"},
			check: func(t *testing.T, cron config.CronAgentConfig) {
				t.Helper()
				if cron.Command != "claude code" {
					t.Fatalf("Command = %q, want claude code", cron.Command)
				}
			},
		},
		{
			args: []string{"groomer", "prompt", "keep", "things", "tidy"},
			check: func(t *testing.T, cron config.CronAgentConfig) {
				t.Helper()
				if cron.Prompt != "keep things tidy" {
					t.Fatalf("Prompt = %q, want keep things tidy", cron.Prompt)
				}
			},
		},
		{
			args: []string{"groomer", "args", "--model", "gpt-5.2"},
			check: func(t *testing.T, cron config.CronAgentConfig) {
				t.Helper()
				if len(cron.Args) != 2 || cron.Args[0] != "--model" || cron.Args[1] != "gpt-5.2" {
					t.Fatalf("Args = %#v, want [--model gpt-5.2]", cron.Args)
				}
			},
		},
	}

	globalFlags.root = s.Root
	for _, tt := range tests {
		cmd := newCronsSetCmd()
		cmd.SetArgs(tt.args)
		if err := cmd.Execute(); err != nil {
			t.Fatalf("Execute %v: %v", tt.args, err)
		}
		tt.check(t, loadCronConfig(t, s.Root))
	}
}

func TestCronsSetArgsDashClearsArgs(t *testing.T) {
	s := newCronCLITestStore(t)

	globalFlags.root = s.Root
	cmd := newCronsSetCmd()
	cmd.SetArgs([]string{"groomer", "args", "-"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	cron := loadCronConfig(t, s.Root)
	if len(cron.Args) != 0 {
		t.Fatalf("Args = %#v, want empty", cron.Args)
	}
}

func TestCronsSetRejectsUnknownAndUnsupportedFields(t *testing.T) {
	s := newCronCLITestStore(t)

	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "unknown",
			args:    []string{"groomer", "bogus", "x"},
			wantErr: `unknown field "bogus"`,
		},
		{
			name:    "unsupported worktree",
			args:    []string{"groomer", "worktree", "true"},
			wantErr: `field "worktree" is not supported here; edit .tickets/config.yml directly`,
		},
		{
			name:    "unsupported base_branch",
			args:    []string{"groomer", "base_branch", "main"},
			wantErr: `field "base_branch" is not supported here; edit .tickets/config.yml directly`,
		},
		{
			name:    "required schedule",
			args:    []string{"groomer", "schedule", "-"},
			wantErr: `field "schedule" is required and cannot be cleared`,
		},
	}

	globalFlags.root = s.Root
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newCronsSetCmd()
			cmd.SetArgs(tt.args)
			err := cmd.Execute()
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func TestCronsCommandsRejectMissingCronName(t *testing.T) {
	s := newCronCLITestStore(t)
	globalFlags.root = s.Root

	tests := []struct {
		name string
		run  func() error
	}{
		{
			name: "enable",
			run: func() error {
				cmd := newCronsEnableCmd()
				cmd.SetArgs([]string{"missing"})
				return cmd.Execute()
			},
		},
		{
			name: "disable",
			run: func() error {
				cmd := newCronsDisableCmd()
				cmd.SetArgs([]string{"missing"})
				return cmd.Execute()
			},
		},
		{
			name: "set",
			run: func() error {
				cmd := newCronsSetCmd()
				cmd.SetArgs([]string{"missing", "schedule", "@every", "5m"})
				return cmd.Execute()
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.run()
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), `cron agent "missing" not configured`) {
				t.Fatalf("error = %v, want missing cron message", err)
			}
		})
	}
}

func TestCronsSetInvalidScheduleReturnsConfigValidationError(t *testing.T) {
	s := newCronCLITestStore(t)

	globalFlags.root = s.Root
	cmd := newCronsSetCmd()
	cmd.SetArgs([]string{"groomer", "schedule", "not-a-schedule"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), `cron agent "groomer" has invalid schedule "not-a-schedule"`) {
		t.Fatalf("error = %v, want invalid schedule message", err)
	}
}
