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
	"github.com/stepandel/tickets-md/internal/stage"
)

// ErrNotFound is returned when a ticket ID cannot be located in any
// stage directory.
var ErrNotFound = errors.New("ticket not found")

// Store is a filesystem-backed collection of tickets. The on-disk
// layout is:
//
//	<root>/.tickets/config.yml
//	<root>/.tickets/<stage>/<ID>.md
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

// Init creates the config file and the stage directories for a brand
// new ticket store.
//
// Init is atomic in the sense that it refuses to write anything to
// disk if a precheck detects a path collision (e.g. a regular file
// already exists where a stage directory needs to go). This avoids
// the surprising "half-initialized" state where config.yml is on
// disk but the stage folders are missing because mkdir failed.
//
// Two-line defense:
//  1. checkInitPaths walks every directory we're about to create and
//     fails fast if any of them collide with a non-directory file.
//  2. Stage directories are created *before* config.yml is written,
//     so even if a check is missed, an init failure leaves at most
//     empty directories rather than an orphaned config.
func Init(root string, c config.Config) (*Store, error) {
	s := &Store{Root: root, Config: c}
	if err := s.checkInitPaths(); err != nil {
		return nil, err
	}
	if err := s.EnsureStageDirs(); err != nil {
		return nil, err
	}
	if err := config.Save(root, c); err != nil {
		return nil, err
	}
	return s, nil
}

// checkInitPaths verifies that none of the directories Init needs to
// create collide with an existing non-directory file. It catches the
// common case where a project already has a binary or file with the
// same name as the store directory (e.g. a stray `.tickets` file in
// the project root).
func (s *Store) checkInitPaths() error {
	paths := []string{filepath.Join(s.Root, config.ConfigDir)}
	for _, stage := range s.Config.Stages {
		paths = append(paths, s.stageDir(stage))
	}
	paths = append(paths, s.projectDir())
	for _, p := range paths {
		if err := mustBeDirOrAbsent(p); err != nil {
			return err
		}
	}
	return nil
}

// mustBeDirOrAbsent returns nil if path either doesn't exist or is
// already a directory, and a descriptive error otherwise.
func mustBeDirOrAbsent(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("cannot create directory %s: a non-directory file already exists at that path", path)
	}
	return nil
}

// EnsureStageDirs creates any missing stage directories under
// <Root>/.tickets.
func (s *Store) EnsureStageDirs() error {
	if err := s.EnsureProjectDir(); err != nil {
		return err
	}
	for _, st := range s.Config.Stages {
		dir := s.stageDir(st)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		if err := stage.WriteDefault(dir); err != nil {
			return err
		}
	}
	return nil
}

// stageDir returns the absolute path to a stage directory.
func (s *Store) stageDir(stage string) string {
	return filepath.Join(s.Root, config.ConfigDir, stage)
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
	if s.Config.IsCompleteStage(toStage) {
		if _, err := s.CompleteUnblock(t.ID); err != nil {
			return Ticket{}, err
		}
		t, err = s.Get(t.ID)
		if err != nil {
			return Ticket{}, err
		}
	}
	return t, nil
}

// Delete removes a ticket from disk. Before removing the file it
// cleans up any link references to this ticket in peer tickets.
func (s *Store) Delete(id string) error {
	t, err := s.Get(id)
	if err != nil {
		return err
	}
	s.cleanupLinks(t)
	return os.Remove(t.Path)
}

// Link creates a bidirectional link between two tickets. linkType is
// one of "related" or "blocked_by". For "related", both tickets gain
// each other in their Related list. For "blocked_by", sourceID gains
// targetID in BlockedBy and targetID gains sourceID in Blocks.
func (s *Store) Link(sourceID, targetID, linkType string) error {
	if sourceID == targetID {
		return fmt.Errorf("cannot link a ticket to itself")
	}
	src, err := s.Get(sourceID)
	if err != nil {
		return err
	}
	tgt, err := s.Get(targetID)
	if err != nil {
		return err
	}

	switch linkType {
	case "related":
		if containsID(src.Related, targetID) {
			return fmt.Errorf("%s and %s are already related", sourceID, targetID)
		}
		src.Related = appendID(src.Related, targetID)
		tgt.Related = appendID(tgt.Related, sourceID)
	case "blocked_by":
		if containsID(src.BlockedBy, targetID) {
			return fmt.Errorf("%s is already blocked by %s", sourceID, targetID)
		}
		src.BlockedBy = appendID(src.BlockedBy, targetID)
		tgt.Blocks = appendID(tgt.Blocks, sourceID)
	case "parent":
		if src.Parent != "" {
			return fmt.Errorf("%s already has parent %s; unlink first", sourceID, src.Parent)
		}
		if err := s.checkParentCycle(sourceID, targetID); err != nil {
			return err
		}
		src.Parent = targetID
		tgt.Children = appendID(tgt.Children, sourceID)
	default:
		return fmt.Errorf("unknown link type %q (use \"related\", \"blocked_by\", or \"parent\")", linkType)
	}

	if err := s.Save(src); err != nil {
		return err
	}
	return s.Save(tgt)
}

// Unlink removes a bidirectional link between two tickets.
func (s *Store) Unlink(sourceID, targetID, linkType string) error {
	src, err := s.Get(sourceID)
	if err != nil {
		return err
	}
	tgt, err := s.Get(targetID)
	if err != nil {
		return err
	}

	switch linkType {
	case "related":
		src.Related = removeID(src.Related, targetID)
		tgt.Related = removeID(tgt.Related, sourceID)
	case "blocked_by":
		src.BlockedBy = removeID(src.BlockedBy, targetID)
		tgt.Blocks = removeID(tgt.Blocks, sourceID)
	case "parent":
		if src.Parent == targetID {
			src.Parent = ""
		}
		tgt.Children = removeID(tgt.Children, sourceID)
	default:
		return fmt.Errorf("unknown link type %q (use \"related\", \"blocked_by\", or \"parent\")", linkType)
	}

	if err := s.Save(src); err != nil {
		return err
	}
	return s.Save(tgt)
}

// cleanupLinks removes all references to t.ID from peer tickets.
// Errors are logged but do not prevent the caller from proceeding.
func (s *Store) cleanupLinks(t Ticket) {
	remove := func(peerID string, mutate func(*Ticket)) {
		peer, err := s.Get(peerID)
		if err != nil {
			return
		}
		mutate(&peer)
		s.Save(peer) // best-effort
	}
	for _, id := range t.Related {
		remove(id, func(p *Ticket) { p.Related = removeID(p.Related, t.ID) })
	}
	for _, id := range t.BlockedBy {
		remove(id, func(p *Ticket) { p.Blocks = removeID(p.Blocks, t.ID) })
	}
	for _, id := range t.Blocks {
		remove(id, func(p *Ticket) { p.BlockedBy = removeID(p.BlockedBy, t.ID) })
	}
	if t.Parent != "" {
		remove(t.Parent, func(p *Ticket) { p.Children = removeID(p.Children, t.ID) })
	}
	for _, id := range t.Children {
		remove(id, func(p *Ticket) {
			if p.Parent == t.ID {
				p.Parent = ""
			}
		})
	}
}

func (s *Store) CompleteUnblock(id string) ([]string, error) {
	t, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if !s.Config.IsCompleteStage(t.Stage) {
		return nil, nil
	}

	touched := make(map[string]struct{})
	knownBlocked := make(map[string]struct{}, len(t.Blocks))

	for _, peerID := range t.Blocks {
		knownBlocked[peerID] = struct{}{}
		peer, err := s.Get(peerID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				continue
			}
			return nil, err
		}
		next := removeID(peer.BlockedBy, t.ID)
		if len(next) == len(peer.BlockedBy) {
			continue
		}
		peer.BlockedBy = next
		if err := s.Save(peer); err != nil {
			return nil, err
		}
		touched[peerID] = struct{}{}
	}

	if len(t.Blocks) > 0 {
		t.Blocks = nil
		if err := s.Save(t); err != nil {
			return nil, err
		}
	}

	all, err := s.ListAll()
	if err != nil {
		return nil, err
	}
	for _, list := range all {
		for _, peer := range list {
			if peer.ID == t.ID || !containsID(peer.BlockedBy, t.ID) {
				continue
			}
			if _, ok := knownBlocked[peer.ID]; ok {
				continue
			}
			peer.BlockedBy = removeID(peer.BlockedBy, t.ID)
			if err := s.Save(peer); err != nil {
				return nil, err
			}
			touched[peer.ID] = struct{}{}
		}
	}

	ids := make([]string, 0, len(touched))
	for peerID := range touched {
		ids = append(ids, peerID)
	}
	sort.Strings(ids)
	return ids, nil
}

func (s *Store) checkParentCycle(childID, parentID string) error {
	all, err := s.ListAll()
	if err != nil {
		return err
	}

	ticketCount := 0
	for _, list := range all {
		ticketCount += len(list)
	}
	if ticketCount == 0 {
		ticketCount = 1
	}

	currentID := parentID
	for i := 0; i < ticketCount; i++ {
		if currentID == childID {
			return fmt.Errorf("cannot parent %s under %s: would create a cycle", childID, parentID)
		}
		current, err := s.Get(currentID)
		if err != nil || current.Parent == "" {
			return nil
		}
		currentID = current.Parent
	}
	return fmt.Errorf("cannot parent %s under %s: would create a cycle", childID, parentID)
}

func containsID(ids []string, id string) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}

func appendID(ids []string, id string) []string {
	if containsID(ids, id) {
		return ids
	}
	return append(ids, id)
}

func removeID(ids []string, id string) []string {
	out := ids[:0]
	for _, v := range ids {
		if v != id {
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
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
