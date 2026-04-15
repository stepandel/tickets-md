package userconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPath_XDG(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("HOME", t.TempDir())

	got, err := Path()
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	want := filepath.Join(xdg, "tickets", "config.yml")
	if got != want {
		t.Fatalf("Path() = %q, want %q", got, want)
	}
}

func TestPath_HomeFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", home)

	got, err := Path()
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	want := filepath.Join(home, ".config", "tickets", "config.yml")
	if got != want {
		t.Fatalf("Path() = %q, want %q", got, want)
	}
}

func TestLoad_Missing(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	got, ok, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if ok {
		t.Fatal("Load() ok = true, want false")
	}
	if got != (UserConfig{}) {
		t.Fatalf("Load() config = %#v, want zero value", got)
	}
}

func TestLoad_ParseError(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("HOME", t.TempDir())
	p := filepath.Join(xdg, "tickets", "config.yml")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(p, []byte("editor: [\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, _, err := Load()
	if err == nil || !strings.Contains(err.Error(), "parsing") {
		t.Fatalf("expected parsing error, got %v", err)
	}
}

func TestSaveThenLoad(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("HOME", t.TempDir())

	want := UserConfig{
		Editor: "code -w",
		UpdateCheck: UpdateCheck{
			LastCheckedAt: time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC),
			LatestVersion: "v0.1.8",
		},
	}
	if err := Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, ok, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !ok {
		t.Fatal("Load() ok = false, want true")
	}
	if got != want {
		t.Fatalf("Load() = %#v, want %#v", got, want)
	}
	if _, err := os.Stat(filepath.Join(xdg, "tickets")); err != nil {
		t.Fatalf("config dir missing: %v", err)
	}
}

func TestSave_CreatesParentDir(t *testing.T) {
	xdg := filepath.Join(t.TempDir(), "deep", "config")
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("HOME", t.TempDir())

	if err := Save(UserConfig{Editor: "vim"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(filepath.Join(xdg, "tickets", "config.yml")); err != nil {
		t.Fatalf("config file missing: %v", err)
	}
}
