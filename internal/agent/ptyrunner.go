package agent

import (
	"fmt"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/creack/pty"
)

// ptySession represents a single running agent process with its PTY.
type ptySession struct {
	name       string
	cmd        *exec.Cmd
	ptmx       *os.File // PTY master fd
	logFile    *os.File
	lastOutput atomic.Int64 // unix epoch of last output

	exitCode *int
	exitErr  error
	copyDone sync.WaitGroup // tracks output-copy goroutine
	done     chan struct{}   // closed when fully finished
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
func (r *PTYRunner) Start(name, cwd string, argv []string, logPath string) error {
	r.mu.Lock()
	if _, exists := r.sessions[name]; exists {
		r.mu.Unlock()
		return fmt.Errorf("session %s already exists", name)
	}
	r.mu.Unlock()

	logF, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("opening log file: %w", err)
	}

	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = cwd

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 24, Cols: 120})
	if err != nil {
		logF.Close()
		return fmt.Errorf("starting pty: %w", err)
	}

	sess := &ptySession{
		name:    name,
		cmd:     cmd,
		ptmx:    ptmx,
		logFile: logF,
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

// copyOutput reads from the PTY master and writes to the log file,
// updating lastOutput on each read. It exits when the PTY returns
// EOF or EIO (child process exited).
func (s *ptySession) copyOutput() {
	buf := make([]byte, 4096)
	for {
		n, err := s.ptmx.Read(buf)
		if n > 0 {
			s.lastOutput.Store(time.Now().Unix())
			s.logFile.Write(buf[:n])
		}
		if err != nil {
			break
		}
	}
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
