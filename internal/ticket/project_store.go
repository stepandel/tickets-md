package ticket

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/stepandel/tickets-md/internal/config"
)

var ErrProjectNotFound = errors.New("project not found")

func (s *Store) EnsureProjectDir() error {
	return os.MkdirAll(s.projectDir(), 0o755)
}

func (s *Store) projectDir() string {
	return filepath.Join(s.Root, config.ConfigDir, "projects")
}

func (s *Store) projectPath(id string) string {
	return filepath.Join(s.projectDir(), id+".md")
}

func (s *Store) projectIDPattern() *regexp.Regexp {
	return regexp.MustCompile("^" + regexp.QuoteMeta(s.Config.ProjectPrefix) + `-(\d+)\.md$`)
}

func (s *Store) ListProjects() ([]Project, error) {
	entries, err := os.ReadDir(s.projectDir())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	pattern := s.projectIDPattern()
	var projects []Project
	for _, e := range entries {
		if e.IsDir() || !pattern.MatchString(e.Name()) {
			continue
		}
		path := filepath.Join(s.projectDir(), e.Name())
		p, err := LoadProjectFile(path)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		projects = append(projects, p)
	}
	sort.Slice(projects, func(i, j int) bool { return projects[i].ID < projects[j].ID })
	return projects, nil
}

func (s *Store) GetProject(id string) (Project, error) {
	id = strings.TrimSuffix(id, ".md")
	path := s.projectPath(id)
	if _, err := os.Stat(path); err == nil {
		return LoadProjectFile(path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return Project{}, err
	}
	return Project{}, fmt.Errorf("%w: %s", ErrProjectNotFound, id)
}

func (s *Store) CreateProject(title string) (Project, error) {
	if strings.TrimSpace(title) == "" {
		return Project{}, errors.New("title is required")
	}
	if err := s.EnsureProjectDir(); err != nil {
		return Project{}, err
	}
	id, err := s.ProjectNextID()
	if err != nil {
		return Project{}, err
	}
	now := time.Now().UTC().Truncate(time.Second)
	p := Project{
		ID:        id,
		Title:     title,
		CreatedAt: now,
		UpdatedAt: now,
		Path:      s.projectPath(id),
		Body:      "## Description\n\n_Describe the project here._\n",
	}
	if err := p.WriteFile(); err != nil {
		return Project{}, err
	}
	return p, nil
}

func (s *Store) SaveProject(p Project) error {
	p.UpdatedAt = time.Now().UTC().Truncate(time.Second)
	return p.WriteFile()
}

func (s *Store) DeleteProject(id string) error {
	p, err := s.GetProject(id)
	if err != nil {
		return err
	}
	all, err := s.ListAll()
	if err != nil {
		return err
	}
	for _, stageTickets := range all {
		for _, t := range stageTickets {
			if t.Project != p.ID {
				continue
			}
			t.Project = ""
			_ = s.Save(t)
		}
	}
	return os.Remove(p.Path)
}

func (s *Store) ProjectNextID() (string, error) {
	entries, err := os.ReadDir(s.projectDir())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Sprintf("%s-%03d", s.Config.ProjectPrefix, 1), nil
		}
		return "", err
	}
	pattern := s.projectIDPattern()
	max := 0
	for _, e := range entries {
		m := pattern.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		n, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		if n > max {
			max = n
		}
	}
	return fmt.Sprintf("%s-%03d", s.Config.ProjectPrefix, max+1), nil
}
