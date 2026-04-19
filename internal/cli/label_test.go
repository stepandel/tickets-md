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

func TestLabelsEditCommandUpdatesConfiguredFields(t *testing.T) {
	s := newCleanupStoreWithConfig(t, config.Config{
		Prefix:        "TIC",
		ProjectPrefix: "PRJ",
		Stages:        []string{"backlog", "execute", "done"},
		Labels: map[string]config.LabelConfig{
			"Backend": {Color: "#0f766e", Bold: true, Order: intPtr(3)},
		},
	})

	globalFlags.root = s.Root
	cmd := newLabelsCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"edit", "backend", "--color", "#123456", "--no-bold", "--order", "7"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	cfg, err := config.Load(s.Root)
	if err != nil {
		t.Fatalf("Load config: %v", err)
	}
	got := cfg.Labels["Backend"]
	if got.Color != "#123456" || got.Bold || got.Order == nil || *got.Order != 7 {
		t.Fatalf("label config = %#v", got)
	}
	if got := strings.TrimSpace(out.String()); got != `Updated label "Backend"` {
		t.Fatalf("output = %q", got)
	}
}

func TestLabelsEditCommandClearsOrder(t *testing.T) {
	s := newCleanupStoreWithConfig(t, config.Config{
		Prefix:        "TIC",
		ProjectPrefix: "PRJ",
		Stages:        []string{"backlog", "execute", "done"},
		Labels: map[string]config.LabelConfig{
			"Backend": {Color: "#0f766e", Order: intPtr(3)},
		},
	})

	globalFlags.root = s.Root
	cmd := newLabelsCmd()
	cmd.SetArgs([]string{"edit", "Backend", "--order", "-"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	cfg, err := config.Load(s.Root)
	if err != nil {
		t.Fatalf("Load config: %v", err)
	}
	if got := cfg.Labels["Backend"].Order; got != nil {
		t.Fatalf("order = %v, want nil", *got)
	}
}

func TestLabelsEditCommandRejectsUnknownLabel(t *testing.T) {
	s := newCleanupStoreWithConfig(t, config.Config{
		Prefix:        "TIC",
		ProjectPrefix: "PRJ",
		Stages:        []string{"backlog", "execute", "done"},
	})

	globalFlags.root = s.Root
	cmd := newLabelsCmd()
	cmd.SetArgs([]string{"edit", "missing", "--color", "#123456"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), `unknown label "missing"`) {
		t.Fatalf("Execute() error = %v, want unknown label", err)
	}
}

func TestLabelsEditCommandRejectsEmptyColor(t *testing.T) {
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
	cmd.SetArgs([]string{"edit", "Backend", "--color", "   "})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), `label "Backend" color is empty`) {
		t.Fatalf("Execute() error = %v, want empty color error", err)
	}
}

func TestLabelsEditCommandRejectsDuplicateOrder(t *testing.T) {
	s := newCleanupStoreWithConfig(t, config.Config{
		Prefix:        "TIC",
		ProjectPrefix: "PRJ",
		Stages:        []string{"backlog", "execute", "done"},
		Labels: map[string]config.LabelConfig{
			"Backend":  {Color: "#0f766e", Order: intPtr(1)},
			"Customer": {Color: "#dc2626", Order: intPtr(2)},
		},
	})

	globalFlags.root = s.Root
	cmd := newLabelsCmd()
	cmd.SetArgs([]string{"edit", "Backend", "--order", "2"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), `label "Customer" order 2 conflicts with "Backend"`) {
		t.Fatalf("Execute() error = %v, want duplicate order error", err)
	}
}

func TestLabelsEditCommandRejectsConflictingBoldFlags(t *testing.T) {
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
	cmd.SetArgs([]string{"edit", "Backend", "--bold", "--no-bold"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), `--bold and --no-bold are mutually exclusive`) {
		t.Fatalf("Execute() error = %v, want conflicting bold flags error", err)
	}
}

func TestLabelsRenameCommandRewritesTicketLabels(t *testing.T) {
	s := newCleanupStoreWithConfig(t, config.Config{
		Prefix:        "TIC",
		ProjectPrefix: "PRJ",
		Stages:        []string{"backlog", "execute", "done"},
		Labels: map[string]config.LabelConfig{
			"backend": {Color: "#0f766e", Bold: true, Order: intPtr(1)},
		},
	})
	first, err := s.Create("First")
	if err != nil {
		t.Fatalf("Create first: %v", err)
	}
	first.Labels = []string{"backend"}
	if err := s.Save(first); err != nil {
		t.Fatalf("Save first: %v", err)
	}
	second, err := s.Create("Second")
	if err != nil {
		t.Fatalf("Create second: %v", err)
	}
	second.Labels = []string{"BACKEND", "Legacy"}
	if err := s.Save(second); err != nil {
		t.Fatalf("Save second: %v", err)
	}

	globalFlags.root = s.Root
	cmd := newLabelsCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"rename", "backend", "Backend"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	cfg, err := config.Load(s.Root)
	if err != nil {
		t.Fatalf("Load config: %v", err)
	}
	if _, ok := cfg.Labels["backend"]; ok {
		t.Fatal("expected old label key to be removed")
	}
	if _, ok := cfg.Labels["Backend"]; !ok {
		t.Fatal("expected renamed label key in config")
	}

	first, err = s.Get(first.ID)
	if err != nil {
		t.Fatalf("Get first: %v", err)
	}
	if strings.Join(first.Labels, ",") != "Backend" {
		t.Fatalf("first labels = %#v", first.Labels)
	}
	second, err = s.Get(second.ID)
	if err != nil {
		t.Fatalf("Get second: %v", err)
	}
	if strings.Join(second.Labels, ",") != "Backend,Legacy" {
		t.Fatalf("second labels = %#v", second.Labels)
	}
	if got := strings.TrimSpace(out.String()); got != `Renamed label "backend" to "Backend" and updated 2 ticket(s)` {
		t.Fatalf("output = %q", got)
	}
}

func TestLabelsRenameCommandRejectsUnknownLabel(t *testing.T) {
	s := newCleanupStoreWithConfig(t, config.Config{
		Prefix:        "TIC",
		ProjectPrefix: "PRJ",
		Stages:        []string{"backlog", "execute", "done"},
	})

	globalFlags.root = s.Root
	cmd := newLabelsCmd()
	cmd.SetArgs([]string{"rename", "missing", "Missing"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), `unknown label "missing"`) {
		t.Fatalf("Execute() error = %v, want unknown label", err)
	}
}

func TestLabelsRenameCommandRejectsSemanticRename(t *testing.T) {
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
	cmd.SetArgs([]string{"rename", "Backend", "Platform"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), `label rename only supports casing changes`) {
		t.Fatalf("Execute() error = %v, want casing-only error", err)
	}
}

func TestLabelsRenameCommandRejectsExactCaseNoop(t *testing.T) {
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
	cmd.SetArgs([]string{"rename", "Backend", "Backend"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), `label "Backend" already uses that casing`) {
		t.Fatalf("Execute() error = %v, want noop error", err)
	}
}

func TestLabelsDeleteCommandRemovesUnusedLabel(t *testing.T) {
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
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"delete", "Backend"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	cfg, err := config.Load(s.Root)
	if err != nil {
		t.Fatalf("Load config: %v", err)
	}
	if _, ok := cfg.Labels["Backend"]; ok {
		t.Fatal("expected Backend label removed from config")
	}
	if got := strings.TrimSpace(out.String()); got != `Deleted label "Backend"` {
		t.Fatalf("output = %q", got)
	}
}

func TestLabelsDeleteCommandIsCaseInsensitive(t *testing.T) {
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
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"delete", "backend"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	cfg, err := config.Load(s.Root)
	if err != nil {
		t.Fatalf("Load config: %v", err)
	}
	if _, ok := cfg.Labels["Backend"]; ok {
		t.Fatal("expected Backend label removed from config")
	}
	if got := strings.TrimSpace(out.String()); got != `Deleted label "Backend"` {
		t.Fatalf("output = %q", got)
	}
}

func TestLabelsDeleteCommandRejectsUnknownLabel(t *testing.T) {
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
	cmd.SetArgs([]string{"delete", "missing"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), `unknown label "missing"`) {
		t.Fatalf("Execute() error = %v, want unknown label", err)
	}
}

func TestLabelsDeleteCommandRejectsReservedNone(t *testing.T) {
	s := newCleanupStoreWithConfig(t, config.Config{
		Prefix:        "TIC",
		ProjectPrefix: "PRJ",
		Stages:        []string{"backlog", "execute", "done"},
	})

	globalFlags.root = s.Root
	cmd := newLabelsCmd()
	cmd.SetArgs([]string{"delete", " none "})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), `label "none" is reserved`) {
		t.Fatalf("Execute() error = %v, want reserved error", err)
	}
}

func TestLabelsDeleteCommandFailsWhenAssignedWithoutForce(t *testing.T) {
	s := newCleanupStoreWithConfig(t, config.Config{
		Prefix:        "TIC",
		ProjectPrefix: "PRJ",
		Stages:        []string{"backlog", "execute", "done"},
		Labels: map[string]config.LabelConfig{
			"Backend": {Color: "#0f766e"},
		},
	})
	first, err := s.Create("First")
	if err != nil {
		t.Fatalf("Create first: %v", err)
	}
	first.Labels = []string{"Backend"}
	if err := s.Save(first); err != nil {
		t.Fatalf("Save first: %v", err)
	}
	second, err := s.Create("Second")
	if err != nil {
		t.Fatalf("Create second: %v", err)
	}
	second.Labels = []string{"Backend", "Legacy"}
	if err := s.Save(second); err != nil {
		t.Fatalf("Save second: %v", err)
	}
	second, err = s.Move(second.ID, "execute")
	if err != nil {
		t.Fatalf("Move second: %v", err)
	}

	globalFlags.root = s.Root
	cmd := newLabelsCmd()
	cmd.SetArgs([]string{"delete", "Backend"})
	err = cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), `label "Backend" is still assigned to 2 ticket(s): `) {
		t.Fatalf("error = %v", err)
	}
	if !strings.Contains(err.Error(), first.ID) || !strings.Contains(err.Error(), second.ID) {
		t.Fatalf("error = %v, want both ticket IDs", err)
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Fatalf("error = %v, want force hint", err)
	}

	cfg, err := config.Load(s.Root)
	if err != nil {
		t.Fatalf("Load config: %v", err)
	}
	if _, ok := cfg.Labels["Backend"]; !ok {
		t.Fatal("expected Backend label to remain configured")
	}
	gotFirst, err := s.Get(first.ID)
	if err != nil {
		t.Fatalf("Get first: %v", err)
	}
	if strings.Join(gotFirst.Labels, ",") != "Backend" {
		t.Fatalf("first labels = %#v, want unchanged", gotFirst.Labels)
	}
	gotSecond, err := s.Get(second.ID)
	if err != nil {
		t.Fatalf("Get second: %v", err)
	}
	if strings.Join(gotSecond.Labels, ",") != "Backend,Legacy" {
		t.Fatalf("second labels = %#v, want unchanged", gotSecond.Labels)
	}
}

func TestLabelsDeleteCommandForceRemovesConfigButKeepsTicketLabels(t *testing.T) {
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
	tk.Labels = []string{"Backend", "Legacy"}
	if err := s.Save(tk); err != nil {
		t.Fatalf("Save: %v", err)
	}

	globalFlags.root = s.Root
	cmd := newLabelsCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"delete", "--force", "Backend"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	cfg, err := config.Load(s.Root)
	if err != nil {
		t.Fatalf("Load config: %v", err)
	}
	if _, ok := cfg.Labels["Backend"]; ok {
		t.Fatal("expected Backend label removed from config")
	}
	got, err := s.Get(tk.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if strings.Join(got.Labels, ",") != "Backend,Legacy" {
		t.Fatalf("labels = %#v, want unchanged", got.Labels)
	}
	if want := `Deleted label "Backend" (still assigned to 1 ticket(s); use ` + "`tickets unlabel <id> Backend`" + ` to remove)`; strings.TrimSpace(out.String()) != want {
		t.Fatalf("output = %q", out.String())
	}
}

func TestLabelsDeleteCommandTruncatesLongCarrierList(t *testing.T) {
	s := newCleanupStoreWithConfig(t, config.Config{
		Prefix:        "TIC",
		ProjectPrefix: "PRJ",
		Stages:        []string{"backlog", "execute", "done"},
		Labels: map[string]config.LabelConfig{
			"Backend": {Color: "#0f766e"},
		},
	})

	var ids []string
	for i := 0; i < 5; i++ {
		tk, err := s.Create("Ticket")
		if err != nil {
			t.Fatalf("Create %d: %v", i, err)
		}
		tk.Labels = []string{"Backend"}
		if err := s.Save(tk); err != nil {
			t.Fatalf("Save %d: %v", i, err)
		}
		ids = append(ids, tk.ID)
	}

	globalFlags.root = s.Root
	cmd := newLabelsCmd()
	cmd.SetArgs([]string{"delete", "Backend"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), strings.Join(ids[:3], ", ")) {
		t.Fatalf("error = %v, want first three ids", err)
	}
	if strings.Contains(err.Error(), ids[3]) || strings.Contains(err.Error(), ids[4]) {
		t.Fatalf("error = %v, did not expect IDs beyond truncation limit", err)
	}
	if !strings.Contains(err.Error(), "(and 2 more)") {
		t.Fatalf("error = %v, want truncation count", err)
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
