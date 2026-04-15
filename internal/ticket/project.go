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

type Project struct {
	ID        string    `yaml:"id"`
	Title     string    `yaml:"title"`
	Status    string    `yaml:"status,omitempty"`
	CreatedAt time.Time `yaml:"created_at"`
	UpdatedAt time.Time `yaml:"updated_at"`

	Body string `yaml:"-"`
	Path string `yaml:"-"`
}

func ParseProject(data []byte, path string) (Project, error) {
	p := Project{Path: path}

	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	if !strings.HasPrefix(text, fmDelim+"\n") && text != fmDelim {
		return p, errors.New("missing frontmatter: file must start with '---'")
	}
	rest := text[len(fmDelim)+1:]

	end := strings.Index(rest, "\n"+fmDelim)
	if end == -1 {
		return p, errors.New("unterminated frontmatter: missing closing '---'")
	}
	header := rest[:end]
	body := rest[end+len("\n"+fmDelim):]
	body = strings.TrimPrefix(body, "\n")

	if err := yaml.Unmarshal([]byte(header), &p); err != nil {
		return p, fmt.Errorf("parsing frontmatter: %w", err)
	}
	p.Body = body
	return p, nil
}

func (p Project) Marshal() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(fmDelim)
	buf.WriteByte('\n')

	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(p); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}

	buf.WriteString(fmDelim)
	buf.WriteByte('\n')

	if p.Body != "" {
		buf.WriteByte('\n')
		buf.WriteString(strings.TrimLeft(p.Body, "\n"))
		if !strings.HasSuffix(p.Body, "\n") {
			buf.WriteByte('\n')
		}
	}
	return buf.Bytes(), nil
}

func LoadProjectFile(path string) (Project, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Project{}, err
	}
	return ParseProject(data, path)
}

func (p Project) WriteFile() error {
	if p.Path == "" {
		return errors.New("project has no path")
	}
	data, err := p.Marshal()
	if err != nil {
		return err
	}
	return os.WriteFile(p.Path, data, 0o644)
}
