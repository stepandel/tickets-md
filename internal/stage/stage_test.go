package stage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingFileReturnsZeroConfig(t *testing.T) {
	dir := t.TempDir()
	c, err := Load(dir)
	if err != nil {
		t.Fatalf("Load missing file: unexpected error %v", err)
	}
	if c.HasAgent() || c.HasCleanup() {
		t.Errorf("zero config should not have agent or cleanup, got %#v", c)
	}
}

func TestLoadAgentConfig(t *testing.T) {
	dir := t.TempDir()
	data := []byte(`agent:
  command: claude
  args: ["--print"]
  prompt: "do the thing"
  worktree: true
  base_branch: main
`)
	if err := os.WriteFile(filepath.Join(dir, ".stage.yml"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !c.HasAgent() {
		t.Fatal("HasAgent should be true")
	}
	if c.Agent.Command != "claude" {
		t.Errorf("command = %q, want claude", c.Agent.Command)
	}
	if !c.Agent.Worktree {
		t.Error("Worktree should be true")
	}
	if c.Agent.BaseBranch != "main" {
		t.Errorf("BaseBranch = %q, want main", c.Agent.BaseBranch)
	}
}

func TestLoadCleanupConfig(t *testing.T) {
	dir := t.TempDir()
	data := []byte(`cleanup:
  worktree: true
  branch: true
`)
	if err := os.WriteFile(filepath.Join(dir, ".stage.yml"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.HasAgent() {
		t.Error("HasAgent should be false")
	}
	if !c.HasCleanup() {
		t.Error("HasCleanup should be true")
	}
	if !c.Cleanup.Worktree || !c.Cleanup.Branch {
		t.Errorf("Cleanup = %#v", c.Cleanup)
	}
}

func TestHasAgentRequiresCommand(t *testing.T) {
	c := Config{Agent: &AgentConfig{Prompt: "x"}}
	if c.HasAgent() {
		t.Error("HasAgent should be false when Command is empty")
	}
}

func TestHasCleanupRequiresAtLeastOneFlag(t *testing.T) {
	c := Config{Cleanup: &CleanupConfig{}}
	if c.HasCleanup() {
		t.Error("HasCleanup should be false when no flags are set")
	}
}

func TestRenderPromptSubstitutesEveryVariable(t *testing.T) {
	tmpl := "id={{id}} title={{title}} path={{path}} stage={{stage}} body={{body}} worktree={{worktree}} links={{links}}"
	got := RenderPrompt(tmpl, PromptVars{
		Path:     "/tmp/t.md",
		ID:       "TIC-001",
		Title:    "hi",
		Stage:    "execute",
		Body:     "the body",
		Worktree: "/tmp/wt",
		Links:    "related: TIC-002",
	})
	want := "id=TIC-001 title=hi path=/tmp/t.md stage=execute body=the body worktree=/tmp/wt links=related: TIC-002"
	if got != want {
		t.Errorf("RenderPrompt =\n  %q\nwant\n  %q", got, want)
	}
}

func TestRenderPromptEmptyVarsLeavesSubstitutedEmptyStrings(t *testing.T) {
	got := RenderPrompt("path={{path}} worktree={{worktree}}", PromptVars{})
	want := "path= worktree="
	if got != want {
		t.Errorf("RenderPrompt = %q, want %q", got, want)
	}
}

func TestLoadRejectsInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".stage.yml"), []byte("this: is: not: yaml"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil {
		t.Error("Load should reject malformed YAML")
	}
}
