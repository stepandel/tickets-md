// Package ticket models a single ticket on disk: a markdown file with
// a YAML frontmatter block. Stage is *not* a field of the ticket — it
// is derived from the parent directory the file lives in.
package ticket

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Ticket is a markdown file with structured YAML metadata.
//
// Stage is the name of the directory the file currently lives in. It
// is filled in by the store at load time and is not persisted into
// the frontmatter (the filesystem is the source of truth for stage).
type Ticket struct {
	ID        string    `yaml:"id"`
	Title     string    `yaml:"title"`
	Priority  string    `yaml:"priority,omitempty"`
	Labels    []string  `yaml:"labels,omitempty"`
	Related   []string  `yaml:"related,omitempty"`
	BlockedBy []string  `yaml:"blocked_by,omitempty"`
	Blocks    []string  `yaml:"blocks,omitempty"`
	Assignee  string    `yaml:"assignee,omitempty"`
	CreatedAt time.Time `yaml:"created_at"`
	UpdatedAt time.Time `yaml:"updated_at"`

	// Agent fields are updated at lifecycle boundaries (spawn and
	// completion) so Obsidian users can see agent state in the
	// frontmatter without consulting .agents/ files.
	AgentStatus  string `yaml:"agent_status,omitempty"`
	AgentRun     string `yaml:"agent_run,omitempty"`
	AgentSession string `yaml:"agent_session,omitempty"`

	// Body is the markdown content after the frontmatter block.
	Body string `yaml:"-"`
	// Stage is the directory name the file lives in. Not persisted.
	Stage string `yaml:"-"`
	// Path is the absolute path to the file on disk. Not persisted.
	Path string `yaml:"-"`
}

// fmDelim is the YAML frontmatter delimiter line.
const fmDelim = "---"

// Parse decodes a markdown file into a Ticket. The file must begin
// with a `---` delimited YAML frontmatter block. stage and path are
// filled into the returned Ticket so callers don't need to set them
// separately.
func Parse(data []byte, stage, path string) (Ticket, error) {
	t := Ticket{Stage: stage, Path: path}

	// Normalize line endings so the splitter handles CRLF too.
	text := strings.ReplaceAll(string(data), "\r\n", "\n")

	if !strings.HasPrefix(text, fmDelim+"\n") && text != fmDelim {
		return t, errors.New("missing frontmatter: file must start with '---'")
	}
	rest := text[len(fmDelim)+1:]

	// Find the closing delimiter, which must be on its own line.
	end := strings.Index(rest, "\n"+fmDelim)
	if end == -1 {
		return t, errors.New("unterminated frontmatter: missing closing '---'")
	}
	header := rest[:end]
	body := rest[end+len("\n"+fmDelim):]
	body = strings.TrimPrefix(body, "\n")

	if err := yaml.Unmarshal([]byte(header), &t); err != nil {
		return t, fmt.Errorf("parsing frontmatter: %w", err)
	}
	t.Body = body
	return t, nil
}

// Marshal encodes a Ticket into the bytes that should be written to
// disk. It always emits a trailing newline.
func (t Ticket) Marshal() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(fmDelim)
	buf.WriteByte('\n')

	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(t); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}

	buf.WriteString(fmDelim)
	buf.WriteByte('\n')

	if t.Body != "" {
		// Ensure exactly one blank line between frontmatter and body
		// for readability, regardless of how the body was stored.
		buf.WriteByte('\n')
		buf.WriteString(strings.TrimLeft(t.Body, "\n"))
		if !strings.HasSuffix(t.Body, "\n") {
			buf.WriteByte('\n')
		}
	}
	return buf.Bytes(), nil
}

// LoadFile reads and parses a ticket file at path, given the stage
// directory it lives in.
func LoadFile(path, stage string) (Ticket, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Ticket{}, err
	}
	return Parse(data, stage, path)
}

// HasLinks reports whether this ticket has any links to other tickets.
func (t Ticket) HasLinks() bool {
	return len(t.Related) > 0 || len(t.BlockedBy) > 0 || len(t.Blocks) > 0
}

// LinkCount returns the total number of links on this ticket.
func (t Ticket) LinkCount() int {
	return len(t.Related) + len(t.BlockedBy) + len(t.Blocks)
}

// LinksText returns a human-readable summary of this ticket's links.
func (t Ticket) LinksText() string {
	var parts []string
	if len(t.Related) > 0 {
		parts = append(parts, "related: "+strings.Join(t.Related, ", "))
	}
	if len(t.BlockedBy) > 0 {
		parts = append(parts, "blocked by: "+strings.Join(t.BlockedBy, ", "))
	}
	if len(t.Blocks) > 0 {
		parts = append(parts, "blocks: "+strings.Join(t.Blocks, ", "))
	}
	return strings.Join(parts, " | ")
}

// WriteFile serializes the ticket and writes it to t.Path, refusing
// if t.Path is empty.
func (t Ticket) WriteFile() error {
	if t.Path == "" {
		return errors.New("ticket has no path")
	}
	data, err := t.Marshal()
	if err != nil {
		return err
	}
	return os.WriteFile(t.Path, data, 0o644)
}
