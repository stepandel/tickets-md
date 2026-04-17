// Package terminal provides a WebSocket server that bridges Obsidian
// (or any WebSocket client) to live PTY sessions managed by PTYRunner.
package terminal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"
)

// Server exposes PTY sessions over WebSocket.
type Server struct {
	runner PTYRunner
	root   string // project root for on-demand agent spawning
	srv    *http.Server
	ln     net.Listener

	// RerunStageAgent, if set, spawns the agent configured on the ticket's
	// current stage (same logic as when a ticket lands in that stage via
	// the file watcher). rows/cols set the initial PTY window size; pass
	// 0 to use the runner's default. Returns the session name on success.
	// Nil means the watcher did not register a callback, so
	// /rerun-stage-agent is rejected.
	RerunStageAgent func(ticketID string, force bool, rows, cols uint16) (string, error)

	// RunCronAgent, if set, manually fires a configured cron agent
	// through the watcher's live PTY runner. Nil means the watcher did
	// not register a callback, so /run-cron-agent is rejected.
	RunCronAgent func(name string, rows, cols uint16) (string, error)

	// TerminateCronSession, if set, stops an active cron PTY session.
	// Nil means the watcher did not register a callback, so
	// /terminate-cron-session is rejected.
	TerminateCronSession func(name string) (string, error)

	// WatchStatus, if set, returns the watcher pause state. Nil means
	// the watcher did not register a callback, so /watch/status is
	// rejected.
	WatchStatus func() (WatchState, error)

	// PauseWatch, if set, pauses watcher-managed spawns. Nil means the
	// watcher did not register a callback, so /watch/pause is rejected.
	PauseWatch func(reason string) (WatchState, error)

	// ResumeWatch, if set, resumes watcher-managed spawns. Nil means the
	// watcher did not register a callback, so /watch/resume is rejected.
	ResumeWatch func() (WatchState, error)
}

var ErrCronRunActive = errors.New("cron run already active")
var ErrCronSessionNotActive = errors.New("cron session not active")

type WatchState struct {
	Paused   bool
	PausedAt time.Time
	Reason   string
	Warning  string
}

// New creates a terminal server backed by the given PTYRunner.
// root is the project root, used to spawn on-demand agent sessions.
func New(runner PTYRunner, root string) *Server {
	return &Server{runner: runner, root: root}
}

// Start listens on a random localhost port and begins serving.
// Returns the chosen port number.
func (s *Server) Start() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("listen: %w", err)
	}
	s.ln = ln

	mux := http.NewServeMux()
	mux.HandleFunc("/terminal/", s.handleTerminal)
	mux.HandleFunc("/sessions", s.handleSessions)
	mux.HandleFunc("/spawn", s.handleSpawn)
	mux.HandleFunc("/rerun-stage-agent", s.handleRerunStageAgent)
	mux.HandleFunc("/run-cron-agent", s.handleRunCronAgent)
	mux.HandleFunc("/terminate-cron-session", s.handleTerminateCronSession)
	mux.HandleFunc("/watch/status", s.handleWatchStatus)
	mux.HandleFunc("/watch/pause", s.handleWatchPause)
	mux.HandleFunc("/watch/resume", s.handleWatchResume)

	s.srv = &http.Server{Handler: withCORS(mux)}
	go s.srv.Serve(ln)

	return ln.Addr().(*net.TCPAddr).Port, nil
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.srv == nil {
		return nil
	}
	return s.srv.Shutdown(ctx)
}

// allowedOrigins is the set of browser origins permitted to call the
// terminal bridge. Non-browser clients (curl, Go tests) send no Origin
// header and are allowed through unconditionally — the bridge only
// listens on 127.0.0.1, so the only realistic attacker is a webpage
// the user visits while `tickets watch` is running.
var allowedOrigins = map[string]bool{
	"app://obsidian.md": true,
}

func originAllowed(origin string) bool {
	if origin == "" {
		return true
	}
	return allowedOrigins[origin]
}

// withCORS restricts cross-origin access to the Obsidian renderer.
// Requests from disallowed browser origins are rejected outright so a
// malicious page cannot spawn agents, read PTY output, or send
// keystrokes.
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if !originAllowed(origin) {
			http.Error(w, "origin not allowed", http.StatusForbidden)
			return
		}
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// parseSize parses rows/cols query string values. Returns ok=false if
// either is missing, non-numeric, zero, or larger than the uint16 PTY
// winsize fields can hold.
func parseSize(rowsStr, colsStr string) (rows, cols uint16, ok bool) {
	if rowsStr == "" || colsStr == "" {
		return 0, 0, false
	}
	r, rerr := strconv.ParseUint(rowsStr, 10, 16)
	c, cerr := strconv.ParseUint(colsStr, 10, 16)
	if rerr != nil || cerr != nil || r == 0 || c == 0 {
		return 0, 0, false
	}
	return uint16(r), uint16(c), true
}

// resizeMsg is the JSON payload for PTY resize requests.
type resizeMsg struct {
	Type string `json:"type"`
	Rows int    `json:"rows"`
	Cols int    `json:"cols"`
}

// handleTerminal upgrades to WebSocket and bridges I/O to a PTY session.
// URL: /terminal/{session-name}
func (s *Server) handleTerminal(w http.ResponseWriter, r *http.Request) {
	sessionName := strings.TrimPrefix(r.URL.Path, "/terminal/")
	if sessionName == "" {
		http.Error(w, "missing session name", http.StatusBadRequest)
		return
	}

	if !s.runner.Alive(sessionName) {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	// Apply ?rows=&cols= before the upgrade so watcher-spawned PTYs
	// (where the geometry isn't known at spawn) resize before the
	// replay buffer is sent — otherwise the first second of output
	// is wrapped at the default 24x120.
	if rows, cols, ok := parseSize(r.URL.Query().Get("rows"), r.URL.Query().Get("cols")); ok {
		s.runner.Resize(sessionName, rows, cols)
	}

	if !originAllowed(r.Header.Get("Origin")) {
		http.Error(w, "origin not allowed", http.StatusForbidden)
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// Origin already verified above against allowedOrigins; skip
		// the library's hostname-only check to avoid duplicating logic.
		InsecureSkipVerify: true,
	})
	if err != nil {
		log.Printf("terminal: websocket accept: %v", err)
		return
	}
	defer conn.CloseNow()

	replay, ch, unsub, err := s.runner.Subscribe(sessionName)
	if err != nil {
		conn.Close(websocket.StatusInternalError, err.Error())
		return
	}
	defer unsub()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Send replay buffer so the client can reconstruct terminal state.
	if len(replay) > 0 {
		if err := conn.Write(ctx, websocket.MessageBinary, replay); err != nil {
			return
		}
	}

	// PTY output → WebSocket (binary frames).
	go func() {
		defer cancel()
		for {
			select {
			case data, ok := <-ch:
				if !ok {
					// Session ended.
					conn.Close(websocket.StatusNormalClosure, "session ended")
					return
				}
				if err := conn.Write(ctx, websocket.MessageBinary, data); err != nil {
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// WebSocket → PTY input (binary) or resize (text JSON).
	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			break
		}
		switch typ {
		case websocket.MessageBinary:
			s.runner.WriteInput(sessionName, data)
		case websocket.MessageText:
			var msg resizeMsg
			if json.Unmarshal(data, &msg) == nil && msg.Type == "resize" && msg.Rows > 0 && msg.Cols > 0 {
				s.runner.Resize(sessionName, uint16(msg.Rows), uint16(msg.Cols))
			}
		}
	}
}

// handleSessions returns a JSON list of active session names.
func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.runner.Sessions())
}

// spawnRequest is the JSON body for POST /spawn and POST /rerun-stage-agent.
// Rows/Cols are optional; when zero the PTY uses the runner's default size
// and resizes once the client sends its first resize message.
type spawnRequest struct {
	TicketID string `json:"ticket_id"`
	Force    bool   `json:"force,omitempty"`
	Rows     uint16 `json:"rows,omitempty"`
	Cols     uint16 `json:"cols,omitempty"`
}

type runCronRequest struct {
	Name string `json:"name"`
	Rows uint16 `json:"rows,omitempty"`
	Cols uint16 `json:"cols,omitempty"`
}

type watchPauseRequest struct {
	Reason string `json:"reason,omitempty"`
}

// spawnResponse is returned by POST /spawn.
type spawnResponse struct {
	Session string `json:"session"`
}

type watchStateResponse struct {
	Paused   bool   `json:"paused"`
	PausedAt string `json:"paused_at,omitempty"`
	Reason   string `json:"reason,omitempty"`
	Warning  string `json:"warning,omitempty"`
}

func writeWatchState(w http.ResponseWriter, state WatchState) {
	resp := watchStateResponse{
		Paused:  state.Paused,
		Reason:  state.Reason,
		Warning: state.Warning,
	}
	if !state.PausedAt.IsZero() {
		resp.PausedAt = state.PausedAt.UTC().Format(time.RFC3339)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleSpawn creates an on-demand interactive agent PTY session
// by running `tickets agents run <ticket-id>`.
func (s *Server) handleSpawn(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req spawnRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.TicketID == "" {
		http.Error(w, "ticket_id is required", http.StatusBadRequest)
		return
	}

	sessionName := "run-" + req.TicketID

	// If a session with this name is already alive, return it.
	if s.runner.Alive(sessionName) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(spawnResponse{Session: sessionName})
		return
	}

	// Resolve the tickets binary path from the running process.
	self, err := os.Executable()
	if err != nil {
		http.Error(w, "cannot resolve executable: "+err.Error(), http.StatusInternalServerError)
		return
	}

	argv := []string{self, "agents", "run", "--root", s.root, req.TicketID}

	// Log to a temp file — the user interacts via the terminal, but
	// we need a path for PTYRunner.Start.
	logDir := filepath.Join(s.root, ".tickets", ".agents", req.TicketID, "runs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		http.Error(w, "creating log dir: "+err.Error(), http.StatusInternalServerError)
		return
	}
	logPath := filepath.Join(logDir, sessionName+".log")

	if err := s.runner.Start(sessionName, s.root, argv, logPath, req.Rows, req.Cols); err != nil {
		http.Error(w, "spawn failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	log.Printf("spawned interactive agent session %s for %s", sessionName, req.TicketID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(spawnResponse{Session: sessionName})
}

// handleRerunStageAgent spawns the agent configured on the ticket's current
// stage. Same flow as when the file watcher sees a ticket land in the stage:
// writes a run YAML, creates a worktree if configured, etc.
func (s *Server) handleRerunStageAgent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.RerunStageAgent == nil {
		http.Error(w, "rerun not available (watcher did not register a callback)", http.StatusServiceUnavailable)
		return
	}

	var req spawnRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.TicketID == "" {
		http.Error(w, "ticket_id is required", http.StatusBadRequest)
		return
	}

	session, err := s.RerunStageAgent(req.TicketID, req.Force, req.Rows, req.Cols)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	log.Printf("re-ran stage agent for %s (session %s)", req.TicketID, session)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(spawnResponse{Session: session})
}

func (s *Server) handleRunCronAgent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.RunCronAgent == nil {
		http.Error(w, "cron run not available (watcher did not register a callback)", http.StatusServiceUnavailable)
		return
	}

	var req runCronRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	session, err := s.RunCronAgent(req.Name, req.Rows, req.Cols)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, ErrCronRunActive) {
			status = http.StatusConflict
		}
		http.Error(w, err.Error(), status)
		return
	}

	log.Printf("ran cron agent %s (session %s)", req.Name, session)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(spawnResponse{Session: session})
}

func (s *Server) handleWatchStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.WatchStatus == nil {
		http.Error(w, "watch status not available (watcher did not register a callback)", http.StatusServiceUnavailable)
		return
	}

	state, err := s.WatchStatus()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeWatchState(w, state)
}

func (s *Server) handleTerminateCronSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.TerminateCronSession == nil {
		http.Error(w, "cron termination not available (watcher did not register a callback)", http.StatusServiceUnavailable)
		return
	}

	var req runCronRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	session, err := s.TerminateCronSession(req.Name)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, ErrCronSessionNotActive) {
			status = http.StatusNotFound
		}
		http.Error(w, err.Error(), status)
		return
	}

	log.Printf("terminated cron agent %s (session %s)", req.Name, session)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(spawnResponse{Session: session})
}

func (s *Server) handleWatchPause(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.PauseWatch == nil {
		http.Error(w, "watch pause not available (watcher did not register a callback)", http.StatusServiceUnavailable)
		return
	}

	var req watchPauseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}

	state, err := s.PauseWatch(req.Reason)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeWatchState(w, state)
}

func (s *Server) handleWatchResume(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.ResumeWatch == nil {
		http.Error(w, "watch resume not available (watcher did not register a callback)", http.StatusServiceUnavailable)
		return
	}

	state, err := s.ResumeWatch()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeWatchState(w, state)
}
