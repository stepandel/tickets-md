package ticket

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestProjectStoreCreateListGetSave(t *testing.T) {
	s := newTestStore(t)

	created, err := s.CreateProject("Spring launch")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if created.ID != "PRJ-001" {
		t.Fatalf("CreateProject ID = %q, want PRJ-001", created.ID)
	}

	list, err := s.ListProjects()
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(list) != 1 || list[0].ID != created.ID {
		t.Fatalf("ListProjects = %#v, want one project %s", list, created.ID)
	}

	got, err := s.GetProject(created.ID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if got.Title != "Spring launch" {
		t.Fatalf("GetProject title = %q", got.Title)
	}

	before := got.UpdatedAt
	time.Sleep(1100 * time.Millisecond)
	got.Status = "active"
	if err := s.SaveProject(got); err != nil {
		t.Fatalf("SaveProject: %v", err)
	}
	got, err = s.GetProject(created.ID)
	if err != nil {
		t.Fatalf("GetProject after save: %v", err)
	}
	if got.Status != "active" {
		t.Fatalf("saved status = %q, want active", got.Status)
	}
	if !got.UpdatedAt.After(before) {
		t.Fatalf("UpdatedAt did not advance: before=%v after=%v", before, got.UpdatedAt)
	}
}

func TestDeleteProjectClearsAssignedTickets(t *testing.T) {
	s := newTestStore(t)
	p, err := s.CreateProject("Spring launch")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	t1, _ := s.Create("Alpha")
	t2, _ := s.Create("Beta")
	t1.Project = p.ID
	t2.Project = p.ID
	if err := s.Save(t1); err != nil {
		t.Fatalf("Save(t1): %v", err)
	}
	if err := s.Save(t2); err != nil {
		t.Fatalf("Save(t2): %v", err)
	}

	if err := s.DeleteProject(p.ID); err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}

	t1, _ = s.Get(t1.ID)
	t2, _ = s.Get(t2.ID)
	if t1.Project != "" || t2.Project != "" {
		t.Fatalf("expected ticket projects cleared, got %q and %q", t1.Project, t2.Project)
	}
	if _, err := s.GetProject(p.ID); err == nil {
		t.Fatal("expected deleted project to be missing")
	}
}

func TestProjectNextIDResumesAfterHoles(t *testing.T) {
	s := newTestStore(t)
	for _, id := range []string{"PRJ-001", "PRJ-004"} {
		path := s.projectPath(id)
		body := "---\nid: " + id + "\ntitle: test\ncreated_at: 2026-04-15T20:00:00Z\nupdated_at: 2026-04-15T20:00:00Z\n---\n"
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("WriteFile(%s): %v", path, err)
		}
	}

	next, err := s.ProjectNextID()
	if err != nil {
		t.Fatalf("ProjectNextID: %v", err)
	}
	if next != "PRJ-005" {
		t.Fatalf("ProjectNextID = %q, want PRJ-005", next)
	}
}

func TestProjectAndTicketCanSharePrefix(t *testing.T) {
	s := newTestStore(t)
	s.Config.ProjectPrefix = s.Config.Prefix

	ticketItem, err := s.Create("Alpha")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	projectItem, err := s.CreateProject("Alpha project")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if !strings.HasPrefix(ticketItem.ID, "T-") || !strings.HasPrefix(projectItem.ID, "T-") {
		t.Fatalf("expected shared prefix IDs, got %s and %s", ticketItem.ID, projectItem.ID)
	}
	if _, err := s.Get(ticketItem.ID); err != nil {
		t.Fatalf("Get(ticket): %v", err)
	}
	if _, err := s.GetProject(projectItem.ID); err != nil {
		t.Fatalf("GetProject(project): %v", err)
	}
}
