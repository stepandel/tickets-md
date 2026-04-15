package updatecheck

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const (
	cacheTTL    = 24 * time.Hour
	releasesURL = "https://api.github.com/repos/stepandel/tickets-md/releases/latest"
)

var latestReleaseURL = releasesURL

type Cache struct {
	LastCheckedAt time.Time
	LatestVersion string
}

type releaseResponse struct {
	TagName string `json:"tag_name"`
}

func Check(ctx context.Context, current string, now time.Time, cache Cache) (Cache, string, error) {
	client := &http.Client{Timeout: 3 * time.Second}
	return check(ctx, client, current, now, cache)
}

func check(ctx context.Context, client *http.Client, current string, now time.Time, cache Cache) (Cache, string, error) {
	if _, ok := parseSemver(current); !ok {
		return cache, "", nil
	}

	now = now.UTC()
	if cacheIsFresh(now, cache) {
		return cache, nagMessage(current, cache.LatestVersion), nil
	}

	nextCache := cache
	nextCache.LastCheckedAt = now

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestReleaseURL, nil)
	if err != nil {
		return nextCache, "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "tickets-md/"+current)

	resp, err := client.Do(req)
	if err != nil {
		return nextCache, "", nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nextCache, "", nil
	}

	var release releaseResponse
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nextCache, "", nil
	}
	if _, ok := parseSemver(release.TagName); !ok {
		return nextCache, "", nil
	}

	nextCache.LatestVersion = release.TagName
	return nextCache, nagMessage(current, nextCache.LatestVersion), nil
}

func cacheIsFresh(now time.Time, cache Cache) bool {
	if cache.LastCheckedAt.IsZero() {
		return false
	}
	return now.Sub(cache.LastCheckedAt.UTC()) < cacheTTL
}

func nagMessage(current, latest string) string {
	if latest == "" {
		return ""
	}
	cmp, ok := compareVersions(current, latest)
	if !ok || cmp >= 0 {
		return ""
	}
	return fmt.Sprintf("tickets: new version %s available (you have %s) — see https://github.com/stepandel/tickets-md/releases/latest", latest, current)
}
