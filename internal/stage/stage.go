// Package stage loads per-stage configuration from .stage.yml files
// inside each stage directory. Stage config is optional — a stage
// without a .stage.yml simply has no agent or other automation.
package stage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const configFile = ".stage.yml"

// Config is the per-stage configuration read from
// .tickets/<stage>/.stage.yml.
type Config struct {
	Agent *AgentConfig `yaml:"agent,omitempty"`
}

// AgentConfig describes a CLI agent to spawn when a ticket arrives
// in this stage.
type AgentConfig struct {
	// Command is the CLI binary to invoke (e.g. "claude", "codex").
	Command string `yaml:"command"`
	// Args are extra CLI flags placed before the rendered prompt
	// (e.g. ["--print"] for `claude --print "<prompt>"`).
	Args []string `yaml:"args,omitempty"`
	// Prompt is a template string rendered with ticket metadata and
	// appended as the final argument. Supported placeholders:
	//   {{path}}      — absolute path to the ticket file
	//   {{id}}        — ticket ID (e.g. TIC-001)
	//   {{title}}     — ticket title from frontmatter
	//   {{stage}}     — destination stage name
	//   {{body}}      — ticket body (markdown after frontmatter)
	//   {{worktree}}  — absolute path to the worktree (empty if worktree is off)
	Prompt string `yaml:"prompt"`
	// Worktree, when true, creates a git worktree per ticket so the
	// agent works in an isolated checkout. The branch is named
	// tickets/<ticket-id>.
	Worktree bool `yaml:"worktree,omitempty"`
	// BaseBranch is the branch to create the worktree from. Defaults
	// to HEAD if empty.
	BaseBranch string `yaml:"base_branch,omitempty"`
}

// HasAgent reports whether this stage is configured to spawn an
// agent when a ticket arrives.
func (c Config) HasAgent() bool {
	return c.Agent != nil && c.Agent.Command != ""
}

// Load reads the .stage.yml from stageDir. A missing file is not an
// error — it returns a zero-value Config with no agent configured.
func Load(stageDir string) (Config, error) {
	p := filepath.Join(stageDir, configFile)
	data, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, nil
		}
		return Config{}, err
	}
	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return Config{}, fmt.Errorf("parsing %s: %w", p, err)
	}
	return c, nil
}

// WriteDefault creates a .stage.yml in stageDir with a commented-out
// agent example so users can see the schema without reading docs. It
// is a no-op if the file already exists.
func WriteDefault(stageDir string) error {
	p := filepath.Join(stageDir, configFile)
	if _, err := os.Stat(p); err == nil {
		return nil // already exists
	}
	return os.WriteFile(p, []byte(defaultStageYML), 0o644)
}

const defaultStageYML = `# Stage configuration — uncomment to enable an agent for this stage.
# When a ticket is moved here, ` + "`tickets watch`" + ` will spawn the agent.
#
# agent:
#   command: claude          # CLI binary (claude, codex, aider, etc.)
#   args: ["--print"]        # extra flags before the prompt
#   worktree: true           # isolate work in a git worktree per ticket
#   base_branch: main        # branch to create worktree from (default: HEAD)
#   prompt: |                # template with {{path}}, {{id}}, {{title}}, {{stage}}, {{body}}, {{worktree}}
#     You are working in {{worktree}} on branch tickets/{{id}}.
#     Read the ticket at {{path}} and implement what it describes.
`

// RenderPrompt replaces template placeholders in the agent prompt
// with concrete ticket values.
func RenderPrompt(prompt string, vars PromptVars) string {
	r := strings.NewReplacer(
		"{{path}}", vars.Path,
		"{{id}}", vars.ID,
		"{{title}}", vars.Title,
		"{{stage}}", vars.Stage,
		"{{body}}", vars.Body,
		"{{worktree}}", vars.Worktree,
	)
	return r.Replace(prompt)
}

// PromptVars holds the values that can be interpolated into an agent
// prompt template.
type PromptVars struct {
	Path     string
	ID       string
	Title    string
	Stage    string
	Body     string
	Worktree string
}
