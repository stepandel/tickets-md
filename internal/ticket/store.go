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

	"tickets-md/internal/config"
)

// ErrNotFound is returned when a ticket ID cannot be located in any
// stage directory.
var ErrNotFound = errors.New("ticket not found")

// Store is a filesystem-backed collection of tickets. The on-disk
// layout is:
//
//	<root>/.tickets/config.yml
//	<root>/<TicketDir>/<stage>/<ID>.md
//
// Stage transitions are implemented as os.Rename between sibling
// directories, so they are atomic on the same filesystem.
type Store struct {
	Root   string
	Config config.Config
}

// Open loads the config from root and returns a ready-to-use Store.
// It does not validate that the stage directories exist on disk —
// callers that need that should use Init or EnsureStageDirs.
func Open(root string) (*Store, error) {
	c, err := config.Load(root)
	if err != nil {
		return nil, err
	}
	return &Store{Root: root, Config: c}, nil
}

// Init creates the config file (if missing) and the stage directories
// for a brand new ticket store.
func Init(root string, c config.Config) (*Store, error) {
	if err := config.Save(root, c); err != nil {
		return nil, err
	}
	s := &Store{Root: root, Config: c}
	if err := s.EnsureStageDirs(); err != nil {
		return nil, err
	}
	return s, nil
}

// EnsureStageDirs creates any missing stage directories under
// <Root>/<TicketDir>.
func (s *Store) EnsureStageDirs() error {
	for _, stage := range s.Config.Stages {
		if err := os.MkdirAll(s.stageDir(stage), 0o755); err != nil {
			return err
		}
	}
	return nil
}

// stageDir returns the absolute path to a stage directory.
func (s *Store) stageDir(stage string) string {
	return filepath.Join(s.Root, s.Config.TicketDir, stage)
}

// ticketPath returns the absolute path of a ticket file in a given
// stage. It does not check whether the file exists.
func (s *Store) ticketPath(stage, id string) string {
	return filepath.Join(s.stageDir(stage), id+".md")
}

// idPattern matches valid ticket IDs of the form PREFIX-NUMBER.
func (s *Store) idPattern() *regexp.Regexp {
	return regexp.MustCompile("^" + regexp.QuoteMeta(s.Config.Prefix) + `-(\d+)\.md$`)
}

// List returns all tickets currently in the given stage, sorted by ID.
func (s *Store) List(stage string) ([]Ticket, error) {
	if !s.Config.HasStage(stage) {
		return nil, fmt.Errorf("unknown stage %q", stage)
	}
	return s.listStage(stage)
}

// ListAll returns every ticket in the store, grouped by stage. Stages
// with no tickets still appear in the map with an empty slice so
// callers can render every column.
func (s *Store) ListAll() (map[string][]Ticket, error) {
	out := make(map[string][]Ticket, len(s.Config.Stages))
	for _, stage := range s.Config.Stages {
		ts, err := s.listStage(stage)
		if err != nil {
			return nil, err
		}
		out[stage] = ts
	}
	return out, nil
}

func (s *Store) listStage(stage string) ([]Ticket, error) {
	dir := s.stageDir(stage)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	pattern := s.idPattern()
	var tickets []Ticket
	for _, e := range entries {
		if e.IsDir() || !pattern.MatchString(e.Name()) {
			continue
		}
		path := filepath.Join(dir, e.Name())
		t, err := LoadFile(path, stage)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		tickets = append(tickets, t)
	}
	sort.Slice(tickets, func(i, j int) bool { return tickets[i].ID < tickets[j].ID })
	return tickets, nil
}

// Get finds a ticket by ID across all stages.
func (s *Store) Get(id string) (Ticket, error) {
	id = strings.TrimSuffix(id, ".md")
	for _, stage := range s.Config.Stages {
		path := s.ticketPath(stage, id)
		if _, err := os.Stat(path); err == nil {
			return LoadFile(path, stage)
		} else if !errors.Is(err, os.ErrNotExist) {
			return Ticket{}, err
		}
	}
	return Ticket{}, fmt.Errorf("%w: %s", ErrNotFound, id)
}

// Create writes a new ticket with the given title into the default
// stage and returns it. The ID is auto-assigned by NextID.
func (s *Store) Create(title string) (Ticket, error) {
	if strings.TrimSpace(title) == "" {
		return Ticket{}, errors.New("title is required")
	}
	if err := s.EnsureStageDirs(); err != nil {
		return Ticket{}, err
	}
	id, err := s.NextID()
	if err != nil {
		return Ticket{}, err
	}
	now := time.Now().UTC().Truncate(time.Second)
	stage := s.Config.DefaultStage()
	t := Ticket{
		ID:        id,
		Title:     title,
		CreatedAt: now,
		UpdatedAt: now,
		Stage:     stage,
		Path:      s.ticketPath(stage, id),
		Body:      "## Description\n\n_Describe the ticket here._\n",
	}
	if err := t.WriteFile(); err != nil {
		return Ticket{}, err
	}
	return t, nil
}

// Save writes back an existing ticket and bumps UpdatedAt. The
// ticket's Path must already be set (i.e. it was loaded from disk or
// returned from Create).
func (s *Store) Save(t Ticket) error {
	t.UpdatedAt = time.Now().UTC().Truncate(time.Second)
	return t.WriteFile()
}

// Move relocates a ticket to a different stage by renaming the file
// between sibling directories. Move is a no-op if the ticket is
// already in the target stage.
func (s *Store) Move(id, toStage string) (Ticket, error) {
	if !s.Config.HasStage(toStage) {
		return Ticket{}, fmt.Errorf("unknown stage %q", toStage)
	}
	t, err := s.Get(id)
	if err != nil {
		return Ticket{}, err
	}
	if t.Stage == toStage {
		return t, nil
	}
	if err := os.MkdirAll(s.stageDir(toStage), 0o755); err != nil {
		return Ticket{}, err
	}
	dst := s.ticketPath(toStage, t.ID)
	if err := os.Rename(t.Path, dst); err != nil {
		return Ticket{}, err
	}
	t.Path = dst
	t.Stage = toStage
	// Touch UpdatedAt so movements are reflected in the metadata too.
	if err := s.Save(t); err != nil {
		return Ticket{}, err
	}
	return t, nil
}

// Delete removes a ticket from disk.
func (s *Store) Delete(id string) error {
	t, err := s.Get(id)
	if err != nil {
		return err
	}
	return os.Remove(t.Path)
}

// NextID scans every stage directory for the highest existing
// numeric suffix and returns the next ID, formatted with at least 3
// digits of zero-padding (e.g. TIC-001, TIC-042, TIC-1234).
func (s *Store) NextID() (string, error) {
	pattern := s.idPattern()
	max := 0
	for _, stage := range s.Config.Stages {
		entries, err := os.ReadDir(s.stageDir(stage))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return "", err
		}
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
	}
	return fmt.Sprintf("%s-%03d", s.Config.Prefix, max+1), nil
}
