// Package obsidian installs the companion Obsidian plugin's build
// artefacts into a vault's plugins directory. Sources are resolved in
// one of two ways:
//
//  1. --from <dir> (for local dev): main.js / manifest.json /
//     styles.css are read from that directory.
//  2. Otherwise: the files are downloaded from the matching
//     GitHub release (`tickets-board-plugin.zip`) and cached under
//     the user's cache directory so repeat installs are offline.
//
// The CLI's own version (from `tickets --version`) picks the release
// tag, which keeps the plugin locked to a version the CLI knows how
// to talk to over the `tickets watch` WebSocket bridge.
package obsidian

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// PluginID is the Obsidian plugin id — must match the id in
// obsidian-plugin/manifest.json. Used to pick the install directory
// name and the community-plugins.json entry.
const PluginID = "tickets-board"

// pluginFiles is the complete set of artefacts that make up the
// plugin. Every source (release zip or --from directory) must supply
// all three.
var pluginFiles = []string{"main.js", "manifest.json", "styles.css"}

// Manifest is the subset of the Obsidian plugin manifest we care
// about. The full manifest is still written verbatim — we only parse
// a few fields for Status() reporting.
type Manifest struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Version       string `json:"version"`
	MinAppVersion string `json:"minAppVersion"`
	Description   string `json:"description"`
	Author        string `json:"author"`
	IsDesktopOnly bool   `json:"isDesktopOnly"`
}

// DiscoverVault walks up from start looking for the nearest directory
// that contains a `.obsidian/` subdirectory. Returns the absolute
// vault path, or an error if nothing was found before reaching the
// filesystem root.
func DiscoverVault(start string) (string, error) {
	abs, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		if info, err := os.Stat(filepath.Join(abs, ".obsidian")); err == nil && info.IsDir() {
			return abs, nil
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return "", fmt.Errorf("no Obsidian vault found at or above %s (looked for a .obsidian/ directory)", start)
		}
		abs = parent
	}
}

// EnsureVault returns a vault rooted at start, creating `.obsidian/`
// there if no ancestor vault exists. Returns the vault path and a
// flag indicating whether the vault was freshly initialized.
func EnsureVault(start string) (string, bool, error) {
	if vault, err := DiscoverVault(start); err == nil {
		return vault, false, nil
	}
	abs, err := filepath.Abs(start)
	if err != nil {
		return "", false, err
	}
	if err := os.MkdirAll(filepath.Join(abs, ".obsidian"), 0o755); err != nil {
		return "", false, fmt.Errorf("initializing Obsidian vault at %s: %w", abs, err)
	}
	return abs, true, nil
}

func pluginDir(vault string) string {
	return filepath.Join(vault, ".obsidian", "plugins", PluginID)
}

// InstallResult summarises what Install did for the caller.
type InstallResult struct {
	Vault            string
	VaultCreated     bool
	Dir              string
	InstalledVersion string
	PreviousVersion  string
	Enabled          bool
	AlreadyEnabled   bool
	// Source describes where the plugin files came from — "release
	// <tag>", "cache <tag>", or "local <dir>" — for user-facing
	// reporting.
	Source string
}

// Install writes the plugin into vault. Files are sourced from
// localDir if non-empty; otherwise they're fetched from the GitHub
// release matching version (cached on disk, so repeat installs are
// offline).
//
// version is the CLI's own version string. "dev" (or "") means the
// caller is running a development build — Install refuses to guess a
// release tag and requires localDir instead.
func Install(vault string, enable bool, version, localDir string) (InstallResult, error) {
	res := InstallResult{Vault: vault}

	srcDir, source, err := resolveSource(version, localDir)
	if err != nil {
		return res, err
	}
	res.Source = source

	if prev, err := readManifest(filepath.Join(pluginDir(vault), "manifest.json")); err == nil {
		res.PreviousVersion = prev.Version
	}

	srcManifest, err := readManifest(filepath.Join(srcDir, "manifest.json"))
	if err != nil {
		return res, fmt.Errorf("reading plugin manifest from %s: %w", srcDir, err)
	}
	res.InstalledVersion = srcManifest.Version

	dir := pluginDir(vault)
	res.Dir = dir
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return res, fmt.Errorf("creating plugin dir: %w", err)
	}
	for _, name := range pluginFiles {
		body, err := os.ReadFile(filepath.Join(srcDir, name))
		if err != nil {
			return res, fmt.Errorf("reading %s from %s: %w", name, srcDir, err)
		}
		if err := writeAtomic(filepath.Join(dir, name), body); err != nil {
			return res, err
		}
	}

	if enable {
		added, err := addCommunityPlugin(vault, PluginID)
		if err != nil {
			return res, err
		}
		res.Enabled = added
		res.AlreadyEnabled = !added
	}
	return res, nil
}

// resolveSource picks the directory to read plugin files from.
func resolveSource(version, localDir string) (dir, source string, err error) {
	if localDir != "" {
		abs, err := filepath.Abs(localDir)
		if err != nil {
			return "", "", err
		}
		if err := ensurePluginFiles(abs); err != nil {
			return "", "", fmt.Errorf("--from %s: %w (run `npm run build` in obsidian-plugin/ first?)", localDir, err)
		}
		return abs, "local " + abs, nil
	}

	tag, err := releaseTag(version)
	if err != nil {
		return "", "", err
	}

	cacheDir, err := pluginCacheDir(tag)
	if err != nil {
		return "", "", err
	}
	if ensurePluginFiles(cacheDir) == nil {
		return cacheDir, "cache " + tag, nil
	}
	if err := downloadRelease(tag, cacheDir); err != nil {
		return "", "", err
	}
	if err := ensurePluginFiles(cacheDir); err != nil {
		return "", "", fmt.Errorf("release %s downloaded but missing expected files: %w", tag, err)
	}
	return cacheDir, "release " + tag, nil
}

// releaseTag normalises version into the GitHub release tag naming.
// "dev" / "" / pseudo-versions are rejected with a pointer at --from.
func releaseTag(version string) (string, error) {
	switch version {
	case "", "dev":
		return "", errors.New("this is a development build (`tickets --version` reports 'dev') so there is no matching GitHub release; pass --from <path-to-obsidian-plugin> to install from a local build, or install a tagged release via Homebrew / `go install ...@vX.Y.Z`")
	}
	// Module pseudo-versions look like v0.0.0-20260414... — there is
	// no corresponding release tag for those either.
	if strings.Contains(version, "-0.") || strings.HasPrefix(version, "v0.0.0-") {
		return "", fmt.Errorf("this binary was built from commit %q which is not a tagged release; pass --from <path-to-obsidian-plugin>, or install a tagged release", version)
	}
	if !strings.HasPrefix(version, "v") {
		return "v" + version, nil
	}
	return version, nil
}

func ensurePluginFiles(dir string) error {
	var missing []string
	for _, name := range pluginFiles {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing %s in %s", strings.Join(missing, ", "), dir)
	}
	return nil
}

// Uninstall removes the plugin directory and drops the plugin id from
// community-plugins.json. Missing pieces are treated as success.
func Uninstall(vault string) error {
	if err := os.RemoveAll(pluginDir(vault)); err != nil {
		return fmt.Errorf("removing plugin dir: %w", err)
	}
	if _, err := removeCommunityPlugin(vault, PluginID); err != nil {
		return err
	}
	return nil
}

// StatusReport captures what Status found in a vault.
type StatusReport struct {
	Vault            string
	Installed        bool
	InstalledVersion string
	ExpectedVersion  string
	Enabled          bool
}

// Status inspects vault without modifying anything. expectedVersion is
// the CLI's own version, used only for the "this CLI expects plugin
// X" line in the status report.
func Status(vault, expectedVersion string) (StatusReport, error) {
	r := StatusReport{Vault: vault, ExpectedVersion: expectedVersion}
	if m, err := readManifest(filepath.Join(pluginDir(vault), "manifest.json")); err == nil {
		r.Installed = true
		r.InstalledVersion = m.Version
	} else if !errors.Is(err, fs.ErrNotExist) {
		return r, err
	}
	enabled, err := hasCommunityPlugin(vault, PluginID)
	if err != nil {
		return r, err
	}
	r.Enabled = enabled
	return r, nil
}

func readManifest(path string) (Manifest, error) {
	var m Manifest
	body, err := os.ReadFile(path)
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(body, &m); err != nil {
		return m, fmt.Errorf("parsing %s: %w", path, err)
	}
	return m, nil
}

func communityPluginsPath(vault string) string {
	return filepath.Join(vault, ".obsidian", "community-plugins.json")
}

func readCommunityPlugins(vault string) ([]string, error) {
	body, err := os.ReadFile(communityPluginsPath(vault))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	body = []byte(strings.TrimSpace(string(body)))
	if len(body) == 0 {
		return nil, nil
	}
	var ids []string
	if err := json.Unmarshal(body, &ids); err != nil {
		return nil, fmt.Errorf("parsing community-plugins.json: %w", err)
	}
	return ids, nil
}

func writeCommunityPlugins(vault string, ids []string) error {
	path := communityPluginsPath(vault)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if ids == nil {
		ids = []string{}
	}
	body, err := json.MarshalIndent(ids, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	return writeAtomic(path, body)
}

// addCommunityPlugin returns true if id was added, false if it was
// already present.
func addCommunityPlugin(vault, id string) (bool, error) {
	ids, err := readCommunityPlugins(vault)
	if err != nil {
		return false, err
	}
	for _, existing := range ids {
		if existing == id {
			return false, nil
		}
	}
	ids = append(ids, id)
	sort.Strings(ids)
	if err := writeCommunityPlugins(vault, ids); err != nil {
		return false, err
	}
	return true, nil
}

// removeCommunityPlugin returns true if the id was found and removed.
func removeCommunityPlugin(vault, id string) (bool, error) {
	ids, err := readCommunityPlugins(vault)
	if err != nil {
		return false, err
	}
	filtered := ids[:0]
	removed := false
	for _, existing := range ids {
		if existing == id {
			removed = true
			continue
		}
		filtered = append(filtered, existing)
	}
	if !removed {
		return false, nil
	}
	if err := writeCommunityPlugins(vault, filtered); err != nil {
		return false, err
	}
	return true, nil
}

func hasCommunityPlugin(vault, id string) (bool, error) {
	ids, err := readCommunityPlugins(vault)
	if err != nil {
		return false, err
	}
	for _, existing := range ids {
		if existing == id {
			return true, nil
		}
	}
	return false, nil
}

func writeAtomic(path string, body []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("creating temp for %s: %w", path, err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("renaming %s: %w", path, err)
	}
	return nil
}
