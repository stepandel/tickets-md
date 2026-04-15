package obsidian

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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

// writeLocalPlugin populates dir with plausible plugin files so
// Install can read from it via --from. Returns dir for chaining.
func writeLocalPlugin(t *testing.T, dir, version string) string {
	t.Helper()
	m := Manifest{ID: PluginID, Name: "Tickets Board", Version: version}
	body, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{
		"manifest.json": body,
		"main.js":       []byte("// test bundle\n"),
		"styles.css":    []byte("/* test */\n"),
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
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

func TestEnsureVaultReturnsExistingWithoutMutating(t *testing.T) {
	vault := newVault(t)
	got, created, err := EnsureVault(vault)
	if err != nil {
		t.Fatalf("EnsureVault: %v", err)
	}
	if created {
		t.Error("existing vault should not be marked as created")
	}
	gotReal, _ := filepath.EvalSymlinks(got)
	wantReal, _ := filepath.EvalSymlinks(vault)
	if gotReal != wantReal {
		t.Errorf("EnsureVault = %s, want %s", gotReal, wantReal)
	}
}

func TestEnsureVaultBootstrapsWhenMissing(t *testing.T) {
	dir := t.TempDir()
	got, created, err := EnsureVault(dir)
	if err != nil {
		t.Fatalf("EnsureVault: %v", err)
	}
	if !created {
		t.Error("missing vault should be marked as created")
	}
	gotReal, _ := filepath.EvalSymlinks(got)
	wantReal, _ := filepath.EvalSymlinks(dir)
	if gotReal != wantReal {
		t.Errorf("EnsureVault = %s, want %s", gotReal, wantReal)
	}
	if info, err := os.Stat(filepath.Join(dir, ".obsidian")); err != nil || !info.IsDir() {
		t.Errorf(".obsidian/ not created: info=%v err=%v", info, err)
	}
}

func TestInstallFromLocalDirWritesFilesAndEnables(t *testing.T) {
	vault := newVault(t)
	src := writeLocalPlugin(t, t.TempDir(), "9.9.9")
	res, err := Install(vault, true, "dev", src)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if res.InstalledVersion != "9.9.9" {
		t.Errorf("InstalledVersion = %q, want 9.9.9", res.InstalledVersion)
	}
	if !res.Enabled {
		t.Error("expected Enabled=true on first install")
	}
	for _, name := range pluginFiles {
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

func TestInstallFromLocalDirRejectsIncompleteSource(t *testing.T) {
	vault := newVault(t)
	src := t.TempDir()
	// Only write one of the three required files.
	if err := os.WriteFile(filepath.Join(src, "main.js"), []byte("// test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(vault, true, "dev", src); err == nil {
		t.Fatal("expected Install to fail when --from dir is missing files")
	}
}

func TestInstallRejectsDevBuildWithoutLocalDir(t *testing.T) {
	vault := newVault(t)
	if _, err := Install(vault, true, "dev", ""); err == nil {
		t.Fatal("expected Install to require --from for dev builds")
	}
	if _, err := Install(vault, true, "", ""); err == nil {
		t.Fatal("expected Install to require --from for empty version")
	}
}

func TestInstallPreservesExistingCommunityPlugins(t *testing.T) {
	vault := newVault(t)
	writeIDs(t, vault, []string{"other-plugin"})
	src := writeLocalPlugin(t, t.TempDir(), "1.0.0")
	if _, err := Install(vault, true, "dev", src); err != nil {
		t.Fatalf("Install: %v", err)
	}
	ids := readIDs(t, vault)
	if len(ids) != 2 || ids[0] != "other-plugin" || ids[1] != PluginID {
		t.Errorf("community-plugins.json = %v, want [other-plugin %s]", ids, PluginID)
	}
}

func TestInstallIdempotent(t *testing.T) {
	vault := newVault(t)
	src := writeLocalPlugin(t, t.TempDir(), "1.0.0")
	if _, err := Install(vault, true, "dev", src); err != nil {
		t.Fatalf("first Install: %v", err)
	}
	res, err := Install(vault, true, "dev", src)
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
	vault := newVault(t)
	src := writeLocalPlugin(t, t.TempDir(), "1.0.0")
	if _, err := Install(vault, false, "dev", src); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if _, err := os.Stat(communityPluginsPath(vault)); !os.IsNotExist(err) {
		t.Errorf("community-plugins.json should not exist when enable=false, got err=%v", err)
	}
}

func TestUninstallRemovesEverything(t *testing.T) {
	vault := newVault(t)
	writeIDs(t, vault, []string{"other-plugin"})
	src := writeLocalPlugin(t, t.TempDir(), "1.0.0")
	if _, err := Install(vault, true, "dev", src); err != nil {
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

func TestStatusReportsInstalledAndExpected(t *testing.T) {
	vault := newVault(t)
	r, err := Status(vault, "v1.2.3")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if r.Installed {
		t.Error("expected Installed=false before install")
	}
	if r.ExpectedVersion != "v1.2.3" {
		t.Errorf("ExpectedVersion = %q, want v1.2.3", r.ExpectedVersion)
	}
	src := writeLocalPlugin(t, t.TempDir(), "1.0.0")
	if _, err := Install(vault, true, "dev", src); err != nil {
		t.Fatalf("Install: %v", err)
	}
	r, err = Status(vault, "v1.2.3")
	if err != nil {
		t.Fatalf("Status after install: %v", err)
	}
	if !r.Installed || r.InstalledVersion != "1.0.0" {
		t.Errorf("Status after install: %+v", r)
	}
	if !r.Enabled {
		t.Error("expected Enabled=true after install")
	}
}

func TestReleaseTagNormalisation(t *testing.T) {
	cases := []struct {
		in, want string
		wantErr  bool
	}{
		{"v0.1.6", "v0.1.6", false},
		{"0.1.6", "v0.1.6", false},
		{"dev", "", true},
		{"", "", true},
		{"v0.0.0-20260414120000-abc123", "", true},
	}
	for _, c := range cases {
		got, err := releaseTag(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("releaseTag(%q) expected error, got %q", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("releaseTag(%q): unexpected error %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("releaseTag(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestInstallDownloadsAndCachesReleaseZip(t *testing.T) {
	// Redirect the release URL to a local test server serving a zip
	// we build on the fly. Point the cache at a temp dir so the test
	// is hermetic.
	zipBody := buildPluginZip(t, "2.0.0")
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/zip")
		io.Copy(w, bytes.NewReader(zipBody))
	}))
	t.Cleanup(srv.Close)

	origURL := releaseURL
	releaseURL = func(tag string) string { return srv.URL + "/" + tag + "/tickets-board-plugin.zip" }
	t.Cleanup(func() { releaseURL = origURL })

	cacheRoot := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheRoot)
	// macOS os.UserCacheDir prefers ~/Library/Caches; override via
	// HOME so the test also covers that branch.
	t.Setenv("HOME", cacheRoot)

	vault := newVault(t)
	res, err := Install(vault, true, "v2.0.0", "")
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if res.InstalledVersion != "2.0.0" {
		t.Errorf("InstalledVersion = %q, want 2.0.0", res.InstalledVersion)
	}
	if hits != 1 {
		t.Errorf("expected 1 download, got %d", hits)
	}

	// Second install should reuse the cache — no new download.
	if _, err := Install(newVault(t), true, "v2.0.0", ""); err != nil {
		t.Fatalf("second Install: %v", err)
	}
	if hits != 1 {
		t.Errorf("expected cached hit on second install, got %d total downloads", hits)
	}
}

func TestInstallSurfaces404AsHelpfulError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	origURL := releaseURL
	releaseURL = func(tag string) string { return srv.URL + "/" + tag + "/tickets-board-plugin.zip" }
	t.Cleanup(func() { releaseURL = origURL })

	cacheRoot := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheRoot)
	t.Setenv("HOME", cacheRoot)

	vault := newVault(t)
	_, err := Install(vault, true, "v99.0.0", "")
	if err == nil {
		t.Fatal("expected error on 404 release fetch")
	}
}

// buildPluginZip returns a zip with the three expected files plus a
// distractor entry to exercise the basename matching path.
func buildPluginZip(t *testing.T, version string) []byte {
	t.Helper()
	m := Manifest{ID: PluginID, Name: "Tickets Board", Version: version}
	manifest, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	entries := map[string][]byte{
		"tickets-board/main.js":       []byte("// release bundle\n"),
		"tickets-board/manifest.json": manifest,
		"tickets-board/styles.css":    []byte("/* release */\n"),
		"tickets-board/README.md":     []byte("ignored\n"),
	}
	for name, body := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
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
