package obsidian

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// releaseURL is the pattern the CLI pulls the plugin zip from. It is
// overridden by tests.
var releaseURL = func(tag string) string {
	return fmt.Sprintf("https://github.com/stepandel/tickets-md/releases/download/%s/tickets-board-plugin.zip", tag)
}

// pluginCacheDir returns the directory the plugin artefacts for tag
// are cached in — typically $XDG_CACHE_HOME/tickets/plugin/<tag>/ on
// Linux, ~/Library/Caches/tickets/plugin/<tag>/ on macOS, etc.
func pluginCacheDir(tag string) (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolving user cache dir: %w", err)
	}
	return filepath.Join(base, "tickets", "plugin", tag), nil
}

// downloadRelease fetches the plugin zip for tag and extracts the
// expected files into dest.
func downloadRelease(tag, dest string) error {
	url := releaseURL(tag)
	client := &http.Client{Timeout: 60 * time.Second}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("building request for %s: %w", url, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("downloading %s: %w (check your network, or pass --from <path> to install a local build)", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("no plugin release found for %s at %s — this tag may predate the standalone plugin release. Install a newer CLI version or pass --from <path>", tag, url)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("downloading %s: HTTP %d", url, resp.StatusCode)
	}

	tmp, err := os.CreateTemp("", "tickets-plugin-*.zip")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		tmp.Close()
		return fmt.Errorf("writing %s: %w", tmpPath, err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return extractPluginZip(tmpPath, dest)
}

// extractPluginZip unpacks the three expected plugin files from zipPath
// into dest. Unknown entries are ignored and path traversal is blocked.
func extractPluginZip(zipPath, dest string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("opening zip %s: %w", zipPath, err)
	}
	defer r.Close()

	wanted := make(map[string]bool, len(pluginFiles))
	for _, name := range pluginFiles {
		wanted[name] = true
	}

	if err := os.MkdirAll(dest, 0o755); err != nil {
		return fmt.Errorf("creating cache dir %s: %w", dest, err)
	}

	found := make(map[string]bool)
	for _, f := range r.File {
		// Entries may be "main.js" or "tickets-board/main.js" depending
		// on how the zip was built — match on the basename so both work.
		base := filepath.Base(f.Name)
		if !wanted[base] || strings.Contains(f.Name, "..") {
			continue
		}
		if err := extractZipEntry(f, filepath.Join(dest, base)); err != nil {
			return err
		}
		found[base] = true
	}

	for _, name := range pluginFiles {
		if !found[name] {
			return fmt.Errorf("plugin zip %s is missing %s", zipPath, name)
		}
	}
	return nil
}

func extractZipEntry(f *zip.File, dest string) error {
	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("opening zip entry %s: %w", f.Name, err)
	}
	defer rc.Close()
	body, err := io.ReadAll(rc)
	if err != nil {
		return fmt.Errorf("reading zip entry %s: %w", f.Name, err)
	}
	return writeAtomic(dest, body)
}
