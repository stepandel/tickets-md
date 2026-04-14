// Package obsidian embeds the Obsidian companion plugin's build
// artefacts and installs them into a vault's plugins directory. It is
// the CLI's side of the "tickets obsidian install" story — a vault
// can get a matched plugin build without going through npm or the
// community directory.
//
// The bundle is populated by `make plugin-bundle`, which runs the
// obsidian-plugin esbuild pipeline and copies main.js / manifest.json
// / styles.css into assets/. A binary built without that step has a
// stub main.js; Install refuses to write it and points the user at
// the Makefile.
package obsidian

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed assets/manifest.json
var manifestBytes []byte

//go:embed assets/main.js
var mainJS []byte

//go:embed assets/styles.css
var stylesCSS []byte

// PluginID is the Obsidian plugin id — must match the id in
// obsidian-plugin/manifest.json. Used to pick the install directory
// name and the community-plugins.json entry.
const PluginID = "tickets-board"

// minBundledMainJS is the smallest size at which we assume main.js was
// populated by `make plugin-bundle` rather than the empty stub that
// ships in the repo. The real bundle is ~500 KB; 4 KB is a safe floor.
const minBundledMainJS = 4 * 1024

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

// BundledManifest returns the manifest embedded in this binary.
func BundledManifest() (Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(manifestBytes, &m); err != nil {
		return Manifest{}, fmt.Errorf("parsing embedded manifest: %w", err)
	}
	return m, nil
}

// HasBundle reports whether this binary actually carries the built
// plugin assets. Binaries produced by a plain `go build` without
// running `make plugin-bundle` first will have the stub main.js and
// return false.
func HasBundle() bool { return len(mainJS) >= minBundledMainJS }

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

// pluginDir returns the destination directory for our plugin inside
// vault.
func pluginDir(vault string) string {
	return filepath.Join(vault, ".obsidian", "plugins", PluginID)
}

// InstallResult summarises what Install did for the caller.
type InstallResult struct {
	Dir              string
	InstalledVersion string
	PreviousVersion  string
	Enabled          bool
	AlreadyEnabled   bool
}

// Install writes the bundled plugin into vault's plugin directory. If
// enable is true it also appends the plugin id to
// `.obsidian/community-plugins.json` so Obsidian activates it on next
// launch.
func Install(vault string, enable bool) (InstallResult, error) {
	var res InstallResult
	if !HasBundle() {
		return res, errors.New("this binary was built without the plugin bundle; run `make plugin-bundle && make install` from source")
	}

	bm, err := BundledManifest()
	if err != nil {
		return res, err
	}
	res.InstalledVersion = bm.Version

	dir := pluginDir(vault)
	res.Dir = dir

	if prev, err := readManifest(filepath.Join(dir, "manifest.json")); err == nil {
		res.PreviousVersion = prev.Version
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return res, fmt.Errorf("creating plugin dir: %w", err)
	}
	files := map[string][]byte{
		"manifest.json": manifestBytes,
		"main.js":       mainJS,
		"styles.css":    stylesCSS,
	}
	for name, body := range files {
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
	BundledVersion   string
	BundledAvailable bool
	Enabled          bool
}

// Status inspects vault without modifying anything.
func Status(vault string) (StatusReport, error) {
	r := StatusReport{Vault: vault, BundledAvailable: HasBundle()}
	if bm, err := BundledManifest(); err == nil {
		r.BundledVersion = bm.Version
	}
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
