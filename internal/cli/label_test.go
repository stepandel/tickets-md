package cli

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stepandel/tickets-md/internal/config"
)

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	prev := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = prev }()
	fn()
	_ = w.Close()
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	return string(data)
}

func TestLabelCommandAddsConfiguredLabelsWithConfiguredCasing(t *testing.T) {
	s := newCleanupStoreWithConfig(t, config.Config{
		Prefix:        "TIC",
		ProjectPrefix: "PRJ",
		Stages:        []string{"backlog", "execute", "done"},
		Labels: map[string]config.LabelConfig{
			"Backend":  {Color: "#0f766e"},
			"Customer": {Color: "#dc2626"},
		},
	})
	tk, err := s.Create("Ticket")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	globalFlags.root = s.Root
	cmd := newLabelCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{tk.ID, "backend", "CUSTOMER"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	got, err := s.Get(tk.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if strings.Join(got.Labels, ",") != "Backend,Customer" {
		t.Fatalf("Labels = %#v, want configured casing", got.Labels)
	}
	if !strings.Contains(out.String(), "Backend, Customer") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestLabelCommandRejectsUnknownLabel(t *testing.T) {
	s := newCleanupStoreWithConfig(t, config.Config{
		Prefix:        "TIC",
		ProjectPrefix: "PRJ",
		Stages:        []string{"backlog", "execute", "done"},
		Labels: map[string]config.LabelConfig{
			"Backend": {Color: "#0f766e"},
		},
	})
	tk, err := s.Create("Ticket")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	globalFlags.root = s.Root
	cmd := newLabelCmd()
	cmd.SetArgs([]string{tk.ID, "missing"})
	err = cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), `unknown label "missing"`) {
		t.Fatalf("Execute() error = %v, want unknown label", err)
	}
}

func TestUnlabelCommandIsIdempotent(t *testing.T) {
	s := newCleanupStoreWithConfig(t, config.Config{
		Prefix:        "TIC",
		ProjectPrefix: "PRJ",
		Stages:        []string{"backlog", "execute", "done"},
	})
	tk, err := s.Create("Ticket")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	tk.Labels = []string{"Backend", "Legacy"}
	if err := s.Save(tk); err != nil {
		t.Fatalf("Save: %v", err)
	}

	globalFlags.root = s.Root
	cmd := newUnlabelCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{tk.ID, "backend", "missing"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	got, err := s.Get(tk.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if strings.Join(got.Labels, ",") != "Legacy" {
		t.Fatalf("Labels = %#v, want Legacy retained", got.Labels)
	}
	if !strings.Contains(out.String(), "Backend") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestLabelsCommandListsConfiguredLabelsInOrder(t *testing.T) {
	s := newCleanupStoreWithConfig(t, config.Config{
		Prefix:        "TIC",
		ProjectPrefix: "PRJ",
		Stages:        []string{"backlog", "execute", "done"},
		Labels: map[string]config.LabelConfig{
			"zzz": {Color: "#111111"},
			"A":   {Color: "#222222"},
			"P1":  {Color: "#333333", Order: intPtr(1)},
		},
	})

	globalFlags.root = s.Root
	cmd := newLabelsCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if got := strings.TrimSpace(out.String()); got != "P1\nA\nzzz" {
		t.Fatalf("output = %q", got)
	}
}

func TestLabelsCreateCommandCreatesConfiguredLabelWithDefaultColor(t *testing.T) {
	s := newCleanupStoreWithConfig(t, config.Config{
		Prefix:        "TIC",
		ProjectPrefix: "PRJ",
		Stages:        []string{"backlog", "execute", "done"},
	})

	globalFlags.root = s.Root
	cmd := newLabelsCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"create", "Backend"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	cfg, err := config.Load(s.Root)
	if err != nil {
		t.Fatalf("Load config: %v", err)
	}
	labelCfg, ok := cfg.Labels["Backend"]
	if !ok {
		t.Fatal("expected Backend label in config")
	}
	if labelCfg.Color != defaultNewLabelColor {
		t.Fatalf("color = %q, want %q", labelCfg.Color, defaultNewLabelColor)
	}
	if got := strings.TrimSpace(out.String()); got != `Created label "Backend" (color `+defaultNewLabelColor+`)` {
		t.Fatalf("output = %q", got)
	}
}

func TestLabelsCreateCommandRejectsNormalizedDuplicate(t *testing.T) {
	s := newCleanupStoreWithConfig(t, config.Config{
		Prefix:        "TIC",
		ProjectPrefix: "PRJ",
		Stages:        []string{"backlog", "execute", "done"},
		Labels: map[string]config.LabelConfig{
			"Backend": {Color: "#0f766e"},
		},
	})

	globalFlags.root = s.Root
	cmd := newLabelsCmd()
	cmd.SetArgs([]string{"create", " backend "})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), `label "backend" already exists as "Backend"`) {
		t.Fatalf("Execute() error = %v, want canonical duplicate error", err)
	}
}

func TestLabelsCreateCommandRejectsReservedNone(t *testing.T) {
	s := newCleanupStoreWithConfig(t, config.Config{
		Prefix:        "TIC",
		ProjectPrefix: "PRJ",
		Stages:        []string{"backlog", "execute", "done"},
	})

	globalFlags.root = s.Root
	cmd := newLabelsCmd()
	cmd.SetArgs([]string{"create", " none "})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), `label "none" is reserved`) {
		t.Fatalf("Execute() error = %v, want reserved error", err)
	}
}

func TestLabelsCommandOnTicketShowsUnknownLabelsAndNone(t *testing.T) {
	s := newCleanupStoreWithConfig(t, config.Config{
		Prefix:        "TIC",
		ProjectPrefix: "PRJ",
		Stages:        []string{"backlog", "execute", "done"},
	})
	tk, err := s.Create("Ticket")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	tk.Labels = []string{"Backend", "Legacy"}
	if err := s.Save(tk); err != nil {
		t.Fatalf("Save: %v", err)
	}

	globalFlags.root = s.Root
	cmd := newLabelsCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--on", tk.ID})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != "Backend\nLegacy" {
		t.Fatalf("output = %q", got)
	}

	empty, err := s.Create("Empty")
	if err != nil {
		t.Fatalf("Create empty: %v", err)
	}
	out.Reset()
	cmd = newLabelsCmd()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--on", empty.ID})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute empty: %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != "(none)" {
		t.Fatalf("output = %q, want (none)", got)
	}
}

func TestListAndShowRenderLabels(t *testing.T) {
	s := newCleanupStoreWithConfig(t, config.Config{
		Prefix:        "TIC",
		ProjectPrefix: "PRJ",
		Stages:        []string{"backlog", "execute", "done"},
	})
	labeled, err := s.Create("Labeled")
	if err != nil {
		t.Fatalf("Create labeled: %v", err)
	}
	labeled.Labels = []string{"Backend", "Customer"}
	if err := s.Save(labeled); err != nil {
		t.Fatalf("Save labeled: %v", err)
	}
	unlabeled, err := s.Create("Unlabeled")
	if err != nil {
		t.Fatalf("Create unlabeled: %v", err)
	}

	globalFlags.root = s.Root
	listOut := captureStdout(t, func() {
		cmd := newListCmd()
		cmd.SetArgs([]string{"--stage", "backlog"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("list Execute: %v", err)
		}
	})
	if !strings.Contains(listOut, "Backend, Customer") {
		t.Fatalf("list output = %q, want labels", listOut)
	}
	if !strings.Contains(listOut, "(none)") {
		t.Fatalf("list output = %q, want unlabeled marker", listOut)
	}

	showOut := captureStdout(t, func() {
		cmd := newShowCmd()
		cmd.SetArgs([]string{unlabeled.ID})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("show Execute unlabeled: %v", err)
		}
	})
	if strings.Contains(showOut, "# Labels:") {
		t.Fatalf("show output = %q, did not expect labels line for unlabeled ticket", showOut)
	}

	showOut = captureStdout(t, func() {
		cmd := newShowCmd()
		cmd.SetArgs([]string{labeled.ID})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("show Execute labeled: %v", err)
		}
	})
	if !strings.Contains(showOut, "# Labels: Backend, Customer") {
		t.Fatalf("show output = %q, want labels line", showOut)
	}
}
