// Package terminal provides a WebSocket server that bridges Obsidian
// (or any WebSocket client) to live PTY sessions managed by PTYRunner.
package terminal

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/coder/websocket"

	"tickets-md/internal/agent"
)

// Server exposes PTY sessions over WebSocket.
type Server struct {
	runner *agent.PTYRunner
	root   string // project root for on-demand agent spawning
	srv    *http.Server
	ln     net.Listener
}

// New creates a terminal server backed by the given PTYRunner.
// root is the project root, used to spawn on-demand agent sessions.
func New(runner *agent.PTYRunner, root string) *Server {
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

// withCORS allows the Obsidian renderer (app://obsidian.md) to POST to
// localhost endpoints. Without this, preflight fails and /spawn silently
// errors in the browser.
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
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

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // localhost only, no origin check needed
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

// spawnRequest is the JSON body for POST /spawn.
type spawnRequest struct {
	TicketID string `json:"ticket_id"`
}

// spawnResponse is returned by POST /spawn.
type spawnResponse struct {
	Session string `json:"session"`
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

	if err := s.runner.Start(sessionName, s.root, argv, logPath); err != nil {
		http.Error(w, "spawn failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	log.Printf("spawned interactive agent session %s for %s", sessionName, req.TicketID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(spawnResponse{Session: sessionName})
}
