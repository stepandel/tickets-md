package ticket

import (
	"strings"
	"testing"
	"time"
)

func TestProjectParseAndMarshalRoundTrip(t *testing.T) {
	created := time.Date(2026, 4, 15, 20, 0, 0, 0, time.UTC)
	updated := created.Add(5 * time.Minute)
	original := Project{
		ID:        "PRJ-001",
		Title:     "Spring launch",
		Status:    "active",
		CreatedAt: created,
		UpdatedAt: updated,
		Body:      "## Description\n\nShip the thing.\n",
		Path:      "/tmp/PRJ-001.md",
	}

	data, err := original.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	got, err := ParseProject(data, original.Path)
	if err != nil {
		t.Fatalf("ParseProject: %v", err)
	}

	if got.ID != original.ID || got.Title != original.Title || got.Status != original.Status {
		t.Fatalf("round-trip metadata mismatch: got %#v want %#v", got, original)
	}
	if !got.CreatedAt.Equal(created) || !got.UpdatedAt.Equal(updated) {
		t.Fatalf("round-trip timestamps mismatch: got %#v", got)
	}
	if strings.TrimLeft(got.Body, "\n") != original.Body {
		t.Fatalf("round-trip body mismatch: got %q want %q", got.Body, original.Body)
	}
}

func TestProjectParseMissingFrontmatter(t *testing.T) {
	_, err := ParseProject([]byte("# no frontmatter\n"), "/tmp/PRJ-001.md")
	if err == nil || !strings.Contains(err.Error(), "missing frontmatter") {
		t.Fatalf("ParseProject error = %v, want missing frontmatter", err)
	}
}

func TestProjectMarshalOmitsEmptyStatus(t *testing.T) {
	p := Project{
		ID:        "PRJ-001",
		Title:     "No status",
		CreatedAt: time.Date(2026, 4, 15, 20, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 4, 15, 20, 0, 0, 0, time.UTC),
	}

	data, err := p.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(data), "status:") {
		t.Fatalf("expected status to be omitted, got:\n%s", string(data))
	}
}
