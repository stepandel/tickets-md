package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"tickets-md/internal/agent"
)

// --- terminal-server client ---

type serverInfo struct {
	Port int `json:"port"`
	Pid  int `json:"pid"`
}

func (m *boardModel) readServer() (*serverInfo, error) {
	data, err := os.ReadFile(terminalServerFilePath(m.store.Root))
	if err != nil {
		return nil, err
	}
	var si serverInfo
	if err := json.Unmarshal(data, &si); err != nil {
		return nil, err
	}
	return &si, nil
}

// postSpawn calls the watcher's terminal server to spawn an agent.
// path is the endpoint (e.g. "/spawn" or "/rerun-stage-agent").
func (m *boardModel) postSpawn(path, ticketID string) (string, error) {
	si, err := m.readServer()
	if err != nil {
		return "", fmt.Errorf("terminal server not running — start `tickets watch`")
	}
	body, _ := json.Marshal(map[string]string{"ticket_id": ticketID})
	client := &http.Client{Timeout: 5 * time.Second}
	url := fmt.Sprintf("http://127.0.0.1:%d%s", si.Port, path)
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		buf := make([]byte, 512)
		n, _ := resp.Body.Read(buf)
		return "", fmt.Errorf("%s: %s", resp.Status, string(buf[:n]))
	}
	var out struct {
		Session string `json:"session"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return out.Session, nil
}

// --- adhoc agent run ---

func (m *boardModel) startAdhocAgent() {
	t, ok := m.selectedTicket()
	if !ok {
		return
	}
	if !m.store.Config.HasDefaultAgent() {
		m.overlay = newNotice("error", "no default_agent in .tickets/config.yml")
		return
	}
	session, err := m.postSpawn("/spawn", t.ID)
	if err != nil {
		m.overlay = newNotice("error", "adhoc: "+err.Error())
		return
	}
	m.overlay = newNotice("info", "spawned adhoc agent — session "+session)
}

// --- re-run stage agent ---

func (m *boardModel) startRerunStageAgent() {
	t, ok := m.selectedTicket()
	if !ok {
		return
	}
	session, err := m.postSpawn("/rerun-stage-agent", t.ID)
	if err != nil {
		m.overlay = newNotice("error", "rerun: "+err.Error())
		return
	}
	m.overlay = newNotice("info", "re-ran stage agent — session "+session)
}

// --- open agent log ---

func (m *boardModel) openAgentLog() {
	t, ok := m.selectedTicket()
	if !ok {
		return
	}
	as, err := agent.Latest(m.store.Root, t.ID)
	if err != nil {
		m.overlay = newNotice("error", "no agent runs for "+t.ID)
		return
	}
	if as.LogFile == "" {
		m.overlay = newNotice("error", "no log file for "+as.RunID)
		return
	}
	name, editorArgs, err := resolveEditor()
	if err != nil {
		m.overlay = newNotice("error", err.Error())
		return
	}
	argv := append([]string{}, editorArgs...)
	argv = append(argv, as.LogFile)
	cmd := exec.Command(name, argv...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		m.overlay = newNotice("error", err.Error())
	}
}

// --- followup ---

func (m *boardModel) startFollowup() {
	t, ok := m.selectedTicket()
	if !ok {
		return
	}
	// Look up the binary that's running us so the subprocess hits the
	// same `tickets` build we're in.
	self, err := os.Executable()
	if err != nil {
		self = "tickets"
	}
	// Spawn detached — interactive agent grabs the TTY and would fight
	// with our alt-screen. Launch in a new terminal window if we can.
	if err := launchInTerminal(self, []string{"agents", "followup", t.ID}, m.store.Root); err != nil {
		m.overlay = newNotice("error", "followup: "+err.Error())
		return
	}
	m.overlay = newNotice("info", "started followup for "+t.ID+" in new terminal")
}

// launchInTerminal opens a new terminal window running `bin args...`
// in cwd. Falls back to a detached process if no terminal emulator is
// available.
func launchInTerminal(bin string, args []string, cwd string) error {
	// macOS: use `open -a Terminal` with a shell script wrapper. Easier
	// to just exec via `osascript`.
	if _, err := exec.LookPath("osascript"); err == nil {
		script := fmt.Sprintf(
			`tell application "Terminal" to do script "cd %s && %s %s"`,
			shellQuote(cwd), shellQuote(bin), shellQuoteArgs(args),
		)
		return exec.Command("osascript", "-e", script).Run()
	}
	// Linux fallback: x-terminal-emulator -e ...
	for _, term := range []string{"x-terminal-emulator", "gnome-terminal", "konsole", "xterm"} {
		if _, err := exec.LookPath(term); err == nil {
			cmd := exec.Command(term, append([]string{"-e", bin}, args...)...)
			cmd.Dir = cwd
			return cmd.Start()
		}
	}
	return fmt.Errorf("no terminal emulator found")
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func shellQuoteArgs(args []string) string {
	parts := make([]string, len(args))
	for i, a := range args {
		parts[i] = shellQuote(a)
	}
	return strings.Join(parts, " ")
}

// --- view diff ---

func (m *boardModel) viewDiff() {
	t, ok := m.selectedTicket()
	if !ok {
		return
	}
	as, err := agent.Latest(m.store.Root, t.ID)
	if err != nil {
		m.overlay = newNotice("error", "no agent runs for "+t.ID)
		return
	}
	dir := as.Worktree
	if dir == "" {
		dir = m.store.Root
	}
	if _, err := os.Stat(dir); err != nil {
		m.overlay = newNotice("error", "worktree missing: "+filepath.Base(dir))
		return
	}
	// Resolve base branch best-effort.
	base := detectBaseBranch(dir)
	// Pipe `git diff` into the user's pager.
	pager := os.Getenv("PAGER")
	if pager == "" {
		if _, err := exec.LookPath("less"); err == nil {
			pager = "less -R"
		} else {
			pager = "cat"
		}
	}
	script := fmt.Sprintf("cd %s && git diff %s...HEAD && git diff | %s",
		shellQuote(dir), shellQuote(base), pager)
	cmd := exec.Command("sh", "-c", script)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		m.overlay = newNotice("error", "diff: "+err.Error())
	}
}

// detectBaseBranch mirrors the logic used by the Obsidian diff view —
// origin/HEAD → main → master → "HEAD".
func detectBaseBranch(dir string) string {
	try := func(args ...string) (string, bool) {
		out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
		if err != nil {
			return "", false
		}
		return string(bytes.TrimSpace(out)), true
	}
	if s, ok := try("symbolic-ref", "--short", "refs/remotes/origin/HEAD"); ok && s != "" {
		return s
	}
	for _, b := range []string{"main", "master"} {
		if _, ok := try("rev-parse", "--verify", b); ok {
			return b
		}
	}
	return "HEAD"
}
