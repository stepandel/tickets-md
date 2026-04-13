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
	"strings"

	"github.com/coder/websocket"

	"tickets-md/internal/agent"
)

// Server exposes PTY sessions over WebSocket.
type Server struct {
	runner *agent.PTYRunner
	srv    *http.Server
	ln     net.Listener
}

// New creates a terminal server backed by the given PTYRunner.
func New(runner *agent.PTYRunner) *Server {
	return &Server{runner: runner}
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

	s.srv = &http.Server{Handler: mux}
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
