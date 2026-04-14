package agent

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/creack/pty"
)

// maxReplayBytes is the amount of recent PTY output kept in memory
// so that new subscribers can reconstruct the terminal state.
const maxReplayBytes = 64 * 1024

// ptySession represents a single running agent process with its PTY.
type ptySession struct {
	name       string
	cmd        *exec.Cmd
	ptmx       *os.File // PTY master fd
	logFile    *os.File
	lastOutput atomic.Int64 // unix epoch of last output

	// Fan-out for PTY output to multiple consumers.
	subMu     sync.Mutex
	subSeq    int
	subs      map[int]chan []byte
	replayBuf []byte // recent output for replay to new subscribers
	// subsClosed is set once closeAllSubs has run so late Subscribe
	// calls return an already-closed channel instead of registering
	// an orphan that would leak a goroutine.
	subsClosed bool

	exitCode *int
	exitErr  error
	copyDone sync.WaitGroup // tracks output-copy goroutine
	done     chan struct{}  // closed when fully finished
}

// PTYRunner manages agent processes in PTYs. It replaces all tmux
// interactions. Safe for concurrent use.
type PTYRunner struct {
	mu       sync.RWMutex
	sessions map[string]*ptySession
}

// NewPTYRunner creates a new runner with an empty session registry.
func NewPTYRunner() *PTYRunner {
	return &PTYRunner{
		sessions: make(map[string]*ptySession),
	}
}

// Start launches a command with a real PTY, capturing all output to
// logPath. The session is registered under name and can be queried
// with Alive/IdleSeconds or stopped with Kill.
//
// rows and cols set the initial PTY window size; pass 0 for either
// to fall back to the default 24x120 (the agent will get a SIGWINCH
// once a client subscribes and sends Resize).
func (r *PTYRunner) Start(name, cwd string, argv []string, logPath string, rows, cols uint16) error {
	r.mu.Lock()
	if _, exists := r.sessions[name]; exists {
		r.mu.Unlock()
		return fmt.Errorf("session %s already exists", name)
	}
	r.mu.Unlock()

	if rows == 0 {
		rows = 24
	}
	if cols == 0 {
		cols = 120
	}

	logF, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("opening log file: %w", err)
	}

	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = cwd

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: rows, Cols: cols})
	if err != nil {
		logF.Close()
		return fmt.Errorf("starting pty: %w", err)
	}

	sess := &ptySession{
		name:    name,
		cmd:     cmd,
		ptmx:    ptmx,
		logFile: logF,
		subs:    make(map[int]chan []byte),
		done:    make(chan struct{}),
	}
	sess.lastOutput.Store(time.Now().Unix())

	r.mu.Lock()
	r.sessions[name] = sess
	r.mu.Unlock()

	sess.copyDone.Add(1)
	go func() {
		defer sess.copyDone.Done()
		sess.copyOutput()
	}()

	go func() {
		sess.waitAndCleanup()
		r.mu.Lock()
		delete(r.sessions, name)
		r.mu.Unlock()
	}()

	return nil
}

// Alive reports whether the named session is still running. Its
// signature matches SessionChecker.
func (r *PTYRunner) Alive(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.sessions[name]
	return ok
}

// IdleSeconds returns how many seconds since the last output on the
// named session's PTY. Returns -1 if the session doesn't exist. Its
// signature matches IdleChecker.
func (r *PTYRunner) IdleSeconds(name string) int {
	r.mu.RLock()
	sess, ok := r.sessions[name]
	r.mu.RUnlock()
	if !ok {
		return -1
	}
	epoch := sess.lastOutput.Load()
	return int(time.Now().Unix() - epoch)
}

// Kill sends SIGTERM to the named session's process.
func (r *PTYRunner) Kill(name string) error {
	r.mu.RLock()
	sess, ok := r.sessions[name]
	r.mu.RUnlock()
	if !ok {
		return fmt.Errorf("session %s not found", name)
	}
	return sess.cmd.Process.Signal(syscall.SIGTERM)
}

// Wait blocks until the named session exits and returns its exit code.
// Returns nil exit code and an error if the session doesn't exist.
func (r *PTYRunner) Wait(name string) (*int, error) {
	r.mu.RLock()
	sess, ok := r.sessions[name]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("session %s not found", name)
	}
	<-sess.done
	return sess.exitCode, sess.exitErr
}

// Subscribe returns a snapshot of recent output (for terminal state
// reconstruction), a channel that receives all future PTY output, and
// an unsubscribe function. The snapshot and channel subscription are
// taken atomically so no data is lost between them. The channel is
// closed when the session ends.
func (r *PTYRunner) Subscribe(name string) ([]byte, <-chan []byte, func(), error) {
	r.mu.RLock()
	sess, ok := r.sessions[name]
	r.mu.RUnlock()
	if !ok {
		return nil, nil, nil, fmt.Errorf("session %s not found", name)
	}

	ch := make(chan []byte, 256)

	sess.subMu.Lock()
	// Snapshot replay buffer while holding lock so no data is missed.
	replay := make([]byte, len(sess.replayBuf))
	copy(replay, sess.replayBuf)
	if sess.subsClosed {
		// Session ended between the map lookup and now. Hand back a
		// closed channel so the caller's range loop terminates instead
		// of blocking forever on an orphan subscription.
		sess.subMu.Unlock()
		close(ch)
		return replay, ch, func() {}, nil
	}
	sess.subSeq++
	id := sess.subSeq
	sess.subs[id] = ch
	sess.subMu.Unlock()

	unsub := func() {
		sess.subMu.Lock()
		delete(sess.subs, id)
		sess.subMu.Unlock()
	}
	return replay, ch, unsub, nil
}

// WriteInput sends data to the named session's PTY stdin.
func (r *PTYRunner) WriteInput(name string, data []byte) (int, error) {
	r.mu.RLock()
	sess, ok := r.sessions[name]
	r.mu.RUnlock()
	if !ok {
		return 0, fmt.Errorf("session %s not found", name)
	}
	return sess.ptmx.Write(data)
}

// Resize changes the PTY window size for the named session.
func (r *PTYRunner) Resize(name string, rows, cols uint16) error {
	r.mu.RLock()
	sess, ok := r.sessions[name]
	r.mu.RUnlock()
	if !ok {
		return fmt.Errorf("session %s not found", name)
	}
	return pty.Setsize(sess.ptmx, &pty.Winsize{Rows: rows, Cols: cols})
}

// Sessions returns the names of all active sessions.
func (r *PTYRunner) Sessions() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.sessions))
	for name := range r.sessions {
		names = append(names, name)
	}
	return names
}

// Shutdown terminates all sessions gracefully (SIGTERM), waiting up
// to 5 seconds before force-killing survivors.
func (r *PTYRunner) Shutdown() {
	r.mu.RLock()
	names := make([]string, 0, len(r.sessions))
	sessions := make([]*ptySession, 0, len(r.sessions))
	for name, sess := range r.sessions {
		names = append(names, name)
		sessions = append(sessions, sess)
	}
	r.mu.RUnlock()

	for _, sess := range sessions {
		sess.cmd.Process.Signal(syscall.SIGTERM)
	}

	done := make(chan struct{})
	go func() {
		for _, name := range names {
			r.Wait(name)
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		r.mu.RLock()
		for _, sess := range r.sessions {
			sess.cmd.Process.Kill()
		}
		r.mu.RUnlock()
	}
}

// fanOut sends a copy of data to all subscribers and appends to the
// replay buffer. Non-blocking: if a subscriber's channel is full, it
// is dropped (channel closed and removed).
func (s *ptySession) fanOut(data []byte) {
	s.subMu.Lock()
	defer s.subMu.Unlock()

	// Keep recent output for replay to late-joining subscribers.
	s.replayBuf = append(s.replayBuf, data...)
	if len(s.replayBuf) > maxReplayBytes {
		s.replayBuf = s.replayBuf[len(s.replayBuf)-maxReplayBytes:]
	}

	for id, ch := range s.subs {
		select {
		case ch <- data:
		default:
			// Slow consumer — drop it.
			close(ch)
			delete(s.subs, id)
		}
	}
}

// closeAllSubs closes all subscriber channels and marks the session
// as no longer accepting new subscribers. Must be called at most once.
func (s *ptySession) closeAllSubs() {
	s.subMu.Lock()
	defer s.subMu.Unlock()
	s.subsClosed = true
	for id, ch := range s.subs {
		close(ch)
		delete(s.subs, id)
	}
}

// copyOutput reads from the PTY master and writes to the log file,
// fanning out to all subscribers and updating lastOutput on each read.
// It exits when the PTY returns EOF or EIO (child process exited).
func (s *ptySession) copyOutput() {
	buf := make([]byte, 4096)
	var logWriteErrLogged bool
	for {
		n, err := s.ptmx.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			s.lastOutput.Store(time.Now().Unix())
			if _, werr := s.logFile.Write(chunk); werr != nil && !logWriteErrLogged {
				// Log once per session — a failing fd will keep failing
				// and we don't want to spam the watcher log. The agent
				// keeps running; only the persisted log is incomplete.
				log.Printf("pty %s: log write failed: %v (log will be truncated)", s.name, werr)
				logWriteErrLogged = true
			}
			s.fanOut(chunk)
		}
		if err != nil {
			break
		}
	}
	s.closeAllSubs()
	s.logFile.Close()
	s.ptmx.Close()
}

// waitAndCleanup waits for the command to exit, ensures all PTY
// output is flushed to the log, and signals completion.
func (s *ptySession) waitAndCleanup() {
	err := s.cmd.Wait()

	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			s.exitErr = err
			s.copyDone.Wait()
			close(s.done)
			return
		}
	}
	s.exitCode = &code

	s.copyDone.Wait()
	close(s.done)
}
