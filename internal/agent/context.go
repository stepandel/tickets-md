package agent

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// RunContext holds the reconstructed context from a previous agent run.
type RunContext struct {
	Diff   string // git diff of changes made (empty if no worktree or no changes)
	Log    string // ANSI-stripped, truncated PTY log output
	Ticket string // ticket markdown body
}

// ansiRegex matches ANSI escape sequences (colors, cursor movement, etc.)
var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\x1b\].*?\x07|\x1b\[.*?[HJK]`)

// StripAnsi removes ANSI escape sequences from s.
func StripAnsi(s string) string {
	return ansiRegex.ReplaceAllString(s, "")
}

// GatherContext reads artifacts from a completed run and returns
// reconstructed context suitable for composing a followup prompt.
// maxLogLines caps the log output to avoid overwhelming the agent.
// maxDiffLines caps the diff output similarly.
func GatherContext(root string, run AgentStatus, ticketPath string, maxLogLines, maxDiffLines int) (RunContext, error) {
	var ctx RunContext

	// Git diff: only available if a worktree was used and still exists.
	if run.Worktree != "" {
		if info, err := os.Stat(run.Worktree); err == nil && info.IsDir() {
			ctx.Diff = gatherDiff(run.Worktree, maxDiffLines)
		}
	}

	// Log: read, strip ANSI, take the last maxLogLines lines.
	if run.LogFile != "" {
		if data, err := os.ReadFile(run.LogFile); err == nil {
			cleaned := strings.TrimSpace(StripAnsi(string(data)))
			if cleaned != "" {
				ctx.Log = tailLines(cleaned, maxLogLines)
			}
		}
	}

	// Ticket body: everything after frontmatter.
	if ticketPath != "" {
		if data, err := os.ReadFile(ticketPath); err == nil {
			ctx.Ticket = extractBody(string(data))
		}
	}

	return ctx, nil
}

// gatherDiff runs git diff against the merge-base with main and returns
// the output, truncated to maxLines. Returns empty string if main has
// no common ancestor with HEAD or the diff command fails.
func gatherDiff(worktree string, maxLines int) string {
	// Find the merge-base with main.
	mbCmd := exec.Command("git", "merge-base", "HEAD", "main")
	mbCmd.Dir = worktree
	mbOut, err := mbCmd.Output()
	if err != nil {
		return ""
	}
	base := strings.TrimSpace(string(mbOut))
	if base == "" {
		return ""
	}

	diffCmd := exec.Command("git", "diff", base+"...HEAD")
	diffCmd.Dir = worktree
	diffOut, err := diffCmd.Output()
	if err != nil {
		return ""
	}
	diff := strings.TrimSpace(string(diffOut))
	if diff == "" {
		return ""
	}
	return truncateLines(diff, maxLines, fmt.Sprintf("(truncated, full diff available in worktree at %s)", worktree))
}

// tailLines returns the last n lines of s.
func tailLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	truncated := lines[len(lines)-n:]
	return "(truncated, showing last " + fmt.Sprintf("%d", n) + " lines)\n" + strings.Join(truncated, "\n")
}

// truncateLines returns the first n lines of s, appending a note if truncated.
func truncateLines(s string, n int, note string) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[:n], "\n") + "\n" + note
}

// extractBody returns the markdown body after YAML frontmatter delimiters.
func extractBody(content string) string {
	const delim = "---"
	text := strings.ReplaceAll(content, "\r\n", "\n")
	if !strings.HasPrefix(text, delim+"\n") {
		return text
	}
	rest := text[len(delim)+1:]
	end := strings.Index(rest, "\n"+delim)
	if end == -1 {
		return text
	}
	body := rest[end+len("\n"+delim):]
	body = strings.TrimPrefix(body, "\n")
	return strings.TrimSpace(body)
}
