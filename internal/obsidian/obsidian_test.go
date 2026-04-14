package obsidian

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func newVault(t *testing.T) string {
	t.Helper()
	vault := t.TempDir()
	if err := os.MkdirAll(filepath.Join(vault, ".obsidian"), 0o755); err != nil {
		t.Fatalf("mkdir .obsidian: %v", err)
	}
	return vault
}

func TestDiscoverVaultWalksUp(t *testing.T) {
	vault := newVault(t)
	nested := filepath.Join(vault, "notes", "inner")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := DiscoverVault(nested)
	if err != nil {
		t.Fatalf("DiscoverVault: %v", err)
	}
	// t.TempDir on macOS returns a path under /private/var, but the
	// starting path may come back as /var. Compare via EvalSymlinks.
	gotReal, _ := filepath.EvalSymlinks(got)
	wantReal, _ := filepath.EvalSymlinks(vault)
	if gotReal != wantReal {
		t.Errorf("DiscoverVault = %s, want %s", gotReal, wantReal)
	}
}

func TestDiscoverVaultFailsWhenMissing(t *testing.T) {
	dir := t.TempDir()
	if _, err := DiscoverVault(dir); err == nil {
		t.Fatal("expected error when no .obsidian directory is found")
	}
}

func TestInstallRefusesWithoutBundle(t *testing.T) {
	if HasBundle() {
		t.Skip("binary includes the real plugin bundle; skipping stub-only assertion")
	}
	vault := newVault(t)
	if _, err := Install(vault, true); err == nil {
		t.Fatal("expected Install to fail when main.js bundle is absent")
	}
}

func TestInstallWritesFilesAndEnables(t *testing.T) {
	if !HasBundle() {
		t.Skip("binary built without the plugin bundle; run `make plugin-bundle` first")
	}
	vault := newVault(t)
	res, err := Install(vault, true)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if res.InstalledVersion == "" {
		t.Error("expected InstalledVersion to be set")
	}
	if !res.Enabled {
		t.Error("expected Enabled=true on first install")
	}
	for _, name := range []string{"manifest.json", "main.js", "styles.css"} {
		path := filepath.Join(vault, ".obsidian", "plugins", PluginID, name)
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("%s is empty", name)
		}
	}
	ids := readIDs(t, vault)
	if len(ids) != 1 || ids[0] != PluginID {
		t.Errorf("community-plugins.json = %v, want [%q]", ids, PluginID)
	}
}

func TestInstallPreservesExistingCommunityPlugins(t *testing.T) {
	if !HasBundle() {
		t.Skip("binary built without the plugin bundle; run `make plugin-bundle` first")
	}
	vault := newVault(t)
	writeIDs(t, vault, []string{"other-plugin"})
	if _, err := Install(vault, true); err != nil {
		t.Fatalf("Install: %v", err)
	}
	ids := readIDs(t, vault)
	if len(ids) != 2 || ids[0] != "other-plugin" || ids[1] != PluginID {
		t.Errorf("community-plugins.json = %v, want [other-plugin %s]", ids, PluginID)
	}
}

func TestInstallIdempotent(t *testing.T) {
	if !HasBundle() {
		t.Skip("binary built without the plugin bundle; run `make plugin-bundle` first")
	}
	vault := newVault(t)
	if _, err := Install(vault, true); err != nil {
		t.Fatalf("first Install: %v", err)
	}
	res, err := Install(vault, true)
	if err != nil {
		t.Fatalf("second Install: %v", err)
	}
	if !res.AlreadyEnabled {
		t.Error("expected AlreadyEnabled=true on reinstall")
	}
	ids := readIDs(t, vault)
	if len(ids) != 1 {
		t.Errorf("expected one entry after reinstall, got %v", ids)
	}
}

func TestInstallNoEnableLeavesCommunityFileAlone(t *testing.T) {
	if !HasBundle() {
		t.Skip("binary built without the plugin bundle; run `make plugin-bundle` first")
	}
	vault := newVault(t)
	if _, err := Install(vault, false); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if _, err := os.Stat(communityPluginsPath(vault)); !os.IsNotExist(err) {
		t.Errorf("community-plugins.json should not exist when enable=false, got err=%v", err)
	}
}

func TestUninstallRemovesEverything(t *testing.T) {
	if !HasBundle() {
		t.Skip("binary built without the plugin bundle; run `make plugin-bundle` first")
	}
	vault := newVault(t)
	writeIDs(t, vault, []string{"other-plugin"})
	if _, err := Install(vault, true); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if err := Uninstall(vault); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if _, err := os.Stat(pluginDir(vault)); !os.IsNotExist(err) {
		t.Errorf("plugin dir still present: %v", err)
	}
	ids := readIDs(t, vault)
	if len(ids) != 1 || ids[0] != "other-plugin" {
		t.Errorf("community-plugins.json = %v, want [other-plugin]", ids)
	}
}

func TestUninstallTolerantOfMissingPieces(t *testing.T) {
	vault := newVault(t)
	if err := Uninstall(vault); err != nil {
		t.Fatalf("Uninstall on clean vault: %v", err)
	}
}

func TestStatusReportsInstalledAndBundled(t *testing.T) {
	if !HasBundle() {
		t.Skip("binary built without the plugin bundle; run `make plugin-bundle` first")
	}
	vault := newVault(t)
	r, err := Status(vault)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if r.Installed {
		t.Error("expected Installed=false before install")
	}
	if r.BundledVersion == "" {
		t.Error("expected BundledVersion to be populated from embedded manifest")
	}
	if _, err := Install(vault, true); err != nil {
		t.Fatalf("Install: %v", err)
	}
	r, err = Status(vault)
	if err != nil {
		t.Fatalf("Status after install: %v", err)
	}
	if !r.Installed || r.InstalledVersion != r.BundledVersion {
		t.Errorf("Status after install: %+v", r)
	}
	if !r.Enabled {
		t.Error("expected Enabled=true after install")
	}
}

func readIDs(t *testing.T, vault string) []string {
	t.Helper()
	body, err := os.ReadFile(communityPluginsPath(vault))
	if err != nil {
		return nil
	}
	var ids []string
	if err := json.Unmarshal(body, &ids); err != nil {
		t.Fatalf("parsing community-plugins.json: %v", err)
	}
	return ids
}

func writeIDs(t *testing.T, vault string, ids []string) {
	t.Helper()
	if err := writeCommunityPlugins(vault, ids); err != nil {
		t.Fatal(err)
	}
}
