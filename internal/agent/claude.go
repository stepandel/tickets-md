package agent

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func init() {
	Register(claudeIntegration{})
}

type claudeIntegration struct{}

func (claudeIntegration) Name() string { return "claude" }

func (claudeIntegration) PrepareArgs(argv []string) ([]string, string, error) {
	id, err := newClaudeSessionID()
	if err != nil {
		return argv, "", err
	}
	return append([]string{"--session-id", id}, argv...), id, nil
}

func (claudeIntegration) PrepareCronArgs(argv []string) ([]string, string, error) {
	argv, id, err := (claudeIntegration{}).PrepareArgs(argv)
	if err != nil {
		return argv, id, err
	}
	if hasClaudePrintFlag(argv) {
		return argv, id, nil
	}
	withPrint := make([]string, 0, len(argv)+1)
	withPrint = append(withPrint, argv[:2]...)
	withPrint = append(withPrint, "--print")
	withPrint = append(withPrint, argv[2:]...)
	return withPrint, id, nil
}

func (claudeIntegration) ExtractPlan(sessionID, cwd string) (string, error) {
	if sessionID == "" {
		return "", nil
	}
	transcript, err := claudeTranscriptPath(sessionID, cwd)
	if err != nil {
		return "", err
	}
	return extractPlanFromClaudeTranscript(transcript)
}

// newClaudeSessionID returns a random v4 UUID suitable for Claude
// Code's --session-id flag. The flag accepts any valid UUID and uses
// it as the transcript filename, giving callers a deterministic path
// to find the session's JSONL.
func newClaudeSessionID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b[:])
	return fmt.Sprintf("%s-%s-%s-%s-%s", h[0:8], h[8:12], h[12:16], h[16:20], h[20:32]), nil
}

// claudeTranscriptPath returns the path where Claude Code stores the
// JSONL transcript for a session started in the given cwd with the
// given session id. Symlinks in cwd are resolved first (macOS /tmp →
// /private/tmp) and every non-alphanumeric rune is encoded as a dash,
// matching Claude Code's own scheme.
func claudeTranscriptPath(sessionID, cwd string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	for _, r := range resolved {
		if (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
			sb.WriteRune(r)
		} else {
			sb.WriteRune('-')
		}
	}
	return filepath.Join(home, ".claude", "projects", sb.String(), sessionID+".jsonl"), nil
}

// extractPlanFromClaudeTranscript scans a Claude Code JSONL transcript
// for the last Write tool call whose file_path lands in
// ~/.claude/plans/ and returns that path. An empty string (with no
// error) means the session produced no plan file.
func extractPlanFromClaudeTranscript(transcriptPath string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	plansPrefix := filepath.Join(home, ".claude", "plans") + string(filepath.Separator)

	f, err := os.Open(transcriptPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var latest string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 16<<20)
	for scanner.Scan() {
		var entry struct {
			Type    string `json:"type"`
			Message struct {
				Content []struct {
					Type  string          `json:"type"`
					Name  string          `json:"name"`
					Input json.RawMessage `json:"input"`
				} `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		if entry.Type != "assistant" {
			continue
		}
		for _, c := range entry.Message.Content {
			if c.Type != "tool_use" || c.Name != "Write" {
				continue
			}
			var input struct {
				FilePath string `json:"file_path"`
			}
			if err := json.Unmarshal(c.Input, &input); err != nil {
				continue
			}
			if strings.HasPrefix(input.FilePath, plansPrefix) {
				latest = input.FilePath
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return latest, nil
}

func hasClaudePrintFlag(argv []string) bool {
	for _, arg := range argv {
		if arg == "--print" || arg == "-p" {
			return true
		}
	}
	return false
}
