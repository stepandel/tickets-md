package updatecheck

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestCheckUsesFreshCacheWithoutHTTP(t *testing.T) {
	now := time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC)
	cache := Cache{
		LastCheckedAt: now.Add(-time.Hour),
		LatestVersion: "v0.1.9",
	}
	gotCache, nag, err := Check(context.Background(), "v0.1.8", now, cache)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if gotCache != cache {
		t.Fatalf("cache = %#v, want %#v", gotCache, cache)
	}
	if !strings.Contains(nag, "v0.1.9") {
		t.Fatalf("nag = %q, want newer version message", nag)
	}
}

func TestCheckRefreshesStaleCache(t *testing.T) {
	var hits int
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			hits++
			if got := r.Header.Get("User-Agent"); got != "tickets-md/v0.1.7" {
				t.Fatalf("User-Agent = %q", got)
			}
			return jsonResponse(http.StatusOK, `{"tag_name":"v0.1.8"}`), nil
		}),
	}
	now := time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC)
	cache := Cache{
		LastCheckedAt: now.Add(-25 * time.Hour),
		LatestVersion: "v0.1.7",
	}
	gotCache, nag, err := check(context.Background(), client, "v0.1.7", now, cache)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if hits != 1 {
		t.Fatalf("expected 1 HTTP call, got %d", hits)
	}
	if gotCache.LastCheckedAt != now {
		t.Fatalf("LastCheckedAt = %v, want %v", gotCache.LastCheckedAt, now)
	}
	if gotCache.LatestVersion != "v0.1.8" {
		t.Fatalf("LatestVersion = %q, want v0.1.8", gotCache.LatestVersion)
	}
	if !strings.Contains(nag, "v0.1.8") {
		t.Fatalf("nag = %q, want newer version message", nag)
	}
}

func TestCheckSilentlySkipsNon200(t *testing.T) {
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusNotFound, `{"message":"not found"}`), nil
		}),
	}
	now := time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC)
	gotCache, nag, err := check(context.Background(), client, "v0.1.7", now, Cache{})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if nag != "" {
		t.Fatalf("nag = %q, want empty", nag)
	}
	if gotCache.LastCheckedAt != now {
		t.Fatalf("LastCheckedAt = %v, want %v", gotCache.LastCheckedAt, now)
	}
}

func TestCheckSilentlySkipsTimeout(t *testing.T) {
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			<-req.Context().Done()
			return nil, req.Context().Err()
		}),
	}
	now := time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	gotCache, nag, err := check(ctx, client, "v0.1.7", now, Cache{})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if nag != "" {
		t.Fatalf("nag = %q, want empty", nag)
	}
	if gotCache.LastCheckedAt != now {
		t.Fatalf("LastCheckedAt = %v, want %v", gotCache.LastCheckedAt, now)
	}
}

func TestCheckNoNagForEqualOrOlder(t *testing.T) {
	now := time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		cache Cache
	}{
		{
			name: "equal",
			cache: Cache{
				LastCheckedAt: now,
				LatestVersion: "v0.1.7",
			},
		},
		{
			name: "older",
			cache: Cache{
				LastCheckedAt: now,
				LatestVersion: "v0.1.6",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, nag, err := Check(context.Background(), "v0.1.7", now, tc.cache)
			if err != nil {
				t.Fatalf("Check: %v", err)
			}
			if nag != "" {
				t.Fatalf("nag = %q, want empty", nag)
			}
		})
	}
}

func TestCheckSilentlySkipsMalformedTag(t *testing.T) {
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, `{"tag_name":"latest"}`), nil
		}),
	}
	now := time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC)
	gotCache, nag, err := check(context.Background(), client, "v0.1.7", now, Cache{})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if nag != "" {
		t.Fatalf("nag = %q, want empty", nag)
	}
	if gotCache.LatestVersion != "" {
		t.Fatalf("LatestVersion = %q, want empty", gotCache.LatestVersion)
	}
}

func TestCheckSkipsDevVersion(t *testing.T) {
	gotCache, nag, err := Check(context.Background(), "dev", time.Now(), Cache{})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if nag != "" {
		t.Fatalf("nag = %q, want empty", nag)
	}
	if gotCache != (Cache{}) {
		t.Fatalf("cache = %#v, want zero", gotCache)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func jsonResponse(statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a, b string
		want int
		ok   bool
	}{
		{"v0.1.7", "v0.1.8", -1, true},
		{"0.1.8", "v0.1.8", 0, true},
		{"v0.2.0", "v0.1.9", 1, true},
		{"v0.1.8-rc1", "v0.1.8", -1, true},
		{"dev", "v0.1.8", 0, false},
		{"main", "v0.1.8", 0, false},
	}

	for _, tc := range tests {
		got, ok := compareVersions(tc.a, tc.b)
		if ok != tc.ok {
			t.Fatalf("compareVersions(%q, %q) ok = %v, want %v", tc.a, tc.b, ok, tc.ok)
		}
		if ok && got != tc.want {
			t.Fatalf("compareVersions(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestCheckPropagatesRequestBuildError(t *testing.T) {
	origURL := latestReleaseURL
	latestReleaseURL = "://bad-url"
	t.Cleanup(func() { latestReleaseURL = origURL })

	_, _, err := Check(context.Background(), "v0.1.7", time.Now(), Cache{})
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
}

func TestCheckIgnoresTransportErrors(t *testing.T) {
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return nil, errors.New("boom")
		}),
	}
	now := time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC)

	gotCache, nag, err := check(context.Background(), client, "v0.1.7", now, Cache{})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if nag != "" {
		t.Fatalf("nag = %q, want empty", nag)
	}
	if gotCache.LastCheckedAt != now {
		t.Fatalf("LastCheckedAt = %v, want %v", gotCache.LastCheckedAt, now)
	}
}
