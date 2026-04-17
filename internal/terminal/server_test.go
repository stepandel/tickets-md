package terminal

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type fakeRunner struct {
	alive        map[string]bool
	sessionsList []string
	replay       []byte
	subCh        chan []byte
	subErr       error
	startErr     error
	startCalls   []startCall
	resizes      []resizeCall
	writes       []writeCall
}

type startCall struct {
	name    string
	cwd     string
	argv    []string
	logPath string
	rows    uint16
	cols    uint16
}

type resizeCall struct {
	name string
	rows uint16
	cols uint16
}

type writeCall struct {
	name string
	data []byte
}

func (f *fakeRunner) Alive(name string) bool {
	return f.alive[name]
}

func (f *fakeRunner) Resize(name string, rows, cols uint16) error {
	f.resizes = append(f.resizes, resizeCall{name: name, rows: rows, cols: cols})
	return nil
}

func (f *fakeRunner) Subscribe(name string) ([]byte, <-chan []byte, func(), error) {
	if f.subErr != nil {
		return nil, nil, nil, f.subErr
	}
	ch := f.subCh
	if ch == nil {
		ch = make(chan []byte)
		close(ch)
	}
	return append([]byte(nil), f.replay...), ch, func() {}, nil
}

func (f *fakeRunner) WriteInput(name string, data []byte) (int, error) {
	buf := append([]byte(nil), data...)
	f.writes = append(f.writes, writeCall{name: name, data: buf})
	return len(data), nil
}

func (f *fakeRunner) Sessions() []string {
	return append([]string(nil), f.sessionsList...)
}

func (f *fakeRunner) Start(name, cwd string, argv []string, logPath string, rows, cols uint16) error {
	f.startCalls = append(f.startCalls, startCall{
		name:    name,
		cwd:     cwd,
		argv:    append([]string(nil), argv...),
		logPath: logPath,
		rows:    rows,
		cols:    cols,
	})
	return f.startErr
}

func newTestHandler(t *testing.T, runner *fakeRunner, root string) http.Handler {
	t.Helper()
	srv := New(runner, root)
	mux := http.NewServeMux()
	mux.HandleFunc("/terminal/", srv.handleTerminal)
	mux.HandleFunc("/sessions", srv.handleSessions)
	mux.HandleFunc("/spawn", srv.handleSpawn)
	mux.HandleFunc("/rerun-stage-agent", srv.handleRerunStageAgent)
	mux.HandleFunc("/run-cron-agent", srv.handleRunCronAgent)
	mux.HandleFunc("/watch/status", srv.handleWatchStatus)
	mux.HandleFunc("/watch/pause", srv.handleWatchPause)
	mux.HandleFunc("/watch/resume", srv.handleWatchResume)
	return withCORS(mux)
}

func responseBody(t *testing.T, rr *httptest.ResponseRecorder) string {
	t.Helper()
	body, err := io.ReadAll(rr.Result().Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	return string(body)
}

func TestCORS_OriginAllowed(t *testing.T) {
	h := newTestHandler(t, &fakeRunner{}, t.TempDir())
	req := httptest.NewRequest(http.MethodGet, "/sessions", nil)
	req.Header.Set("Origin", "app://obsidian.md")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "app://obsidian.md" {
		t.Fatalf("Allow-Origin = %q", got)
	}
}

func TestCORS_OriginDenied(t *testing.T) {
	h := newTestHandler(t, &fakeRunner{}, t.TempDir())
	req := httptest.NewRequest(http.MethodGet, "/sessions", nil)
	req.Header.Set("Origin", "https://evil.example")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rr.Code)
	}
}

func TestCORS_EmptyOrigin(t *testing.T) {
	h := newTestHandler(t, &fakeRunner{}, t.TempDir())
	req := httptest.NewRequest(http.MethodGet, "/sessions", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
}

func TestCORS_PreflightOptions(t *testing.T) {
	h := newTestHandler(t, &fakeRunner{}, t.TempDir())
	req := httptest.NewRequest(http.MethodOptions, "/sessions", nil)
	req.Header.Set("Origin", "app://obsidian.md")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rr.Code)
	}
	if got := rr.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Fatal("missing Access-Control-Allow-Methods")
	}
}

func TestSessions(t *testing.T) {
	h := newTestHandler(t, &fakeRunner{sessionsList: []string{"run-TIC-1", "run-TIC-2"}}, t.TempDir())
	req := httptest.NewRequest(http.MethodGet, "/sessions", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	var got []string
	if err := json.NewDecoder(rr.Result().Body).Decode(&got); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	want := []string{"run-TIC-1", "run-TIC-2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sessions = %#v, want %#v", got, want)
	}
}

func TestSpawn_BadMethod(t *testing.T) {
	h := newTestHandler(t, &fakeRunner{}, t.TempDir())
	req := httptest.NewRequest(http.MethodGet, "/spawn", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rr.Code)
	}
}

func TestSpawn_BadJSON(t *testing.T) {
	h := newTestHandler(t, &fakeRunner{}, t.TempDir())
	req := httptest.NewRequest(http.MethodPost, "/spawn", strings.NewReader("{"))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	body := responseBody(t, rr)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
	if !strings.Contains(body, "bad request") {
		t.Fatalf("body = %q", body)
	}
}

func TestSpawn_MissingTicketID(t *testing.T) {
	h := newTestHandler(t, &fakeRunner{}, t.TempDir())
	req := httptest.NewRequest(http.MethodPost, "/spawn", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	body := responseBody(t, rr)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
	if !strings.Contains(body, "ticket_id is required") {
		t.Fatalf("body = %q", body)
	}
}

func TestSpawn_AlreadyAlive(t *testing.T) {
	runner := &fakeRunner{alive: map[string]bool{"run-TIC-9": true}}
	h := newTestHandler(t, runner, t.TempDir())
	req := httptest.NewRequest(http.MethodPost, "/spawn", strings.NewReader(`{"ticket_id":"TIC-9"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	var got spawnResponse
	if err := json.NewDecoder(rr.Result().Body).Decode(&got); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Session != "run-TIC-9" {
		t.Fatalf("session = %q", got.Session)
	}
	if len(runner.startCalls) != 0 {
		t.Fatalf("Start called %d times, want 0", len(runner.startCalls))
	}
}

func TestSpawn_Success(t *testing.T) {
	root := t.TempDir()
	runner := &fakeRunner{alive: map[string]bool{}}
	h := newTestHandler(t, runner, root)
	req := httptest.NewRequest(http.MethodPost, "/spawn", strings.NewReader(`{"ticket_id":"TIC-9","rows":40,"cols":132}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	var got spawnResponse
	if err := json.NewDecoder(rr.Result().Body).Decode(&got); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Session != "run-TIC-9" {
		t.Fatalf("session = %q", got.Session)
	}
	if len(runner.startCalls) != 1 {
		t.Fatalf("Start called %d times, want 1", len(runner.startCalls))
	}
	call := runner.startCalls[0]
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("Executable: %v", err)
	}
	wantArgv := []string{self, "agents", "run", "--root", root, "TIC-9"}
	if !reflect.DeepEqual(call.argv, wantArgv) {
		t.Fatalf("argv = %#v, want %#v", call.argv, wantArgv)
	}
	if call.rows != 40 || call.cols != 132 {
		t.Fatalf("rows/cols = %d/%d", call.rows, call.cols)
	}
	logDir := filepath.Join(root, ".tickets", ".agents", "TIC-9", "runs")
	if _, err := os.Stat(logDir); err != nil {
		t.Fatalf("log dir missing: %v", err)
	}
}

func TestSpawn_StartFails(t *testing.T) {
	runner := &fakeRunner{alive: map[string]bool{}, startErr: errors.New("boom")}
	h := newTestHandler(t, runner, t.TempDir())
	req := httptest.NewRequest(http.MethodPost, "/spawn", strings.NewReader(`{"ticket_id":"TIC-9"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	body := responseBody(t, rr)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rr.Code)
	}
	if !strings.Contains(body, "spawn failed") {
		t.Fatalf("body = %q", body)
	}
}

func TestRerun_NoCallback(t *testing.T) {
	h := newTestHandler(t, &fakeRunner{}, t.TempDir())
	req := httptest.NewRequest(http.MethodPost, "/rerun-stage-agent", strings.NewReader(`{"ticket_id":"TIC-5"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rr.Code)
	}
}

func TestRerun_BadJSON(t *testing.T) {
	root := t.TempDir()
	srv := New(&fakeRunner{}, root)
	srv.RerunStageAgent = func(ticketID string, force bool, rows, cols uint16) (string, error) { return "", nil }
	mux := http.NewServeMux()
	mux.HandleFunc("/rerun-stage-agent", srv.handleRerunStageAgent)
	h := withCORS(mux)
	req := httptest.NewRequest(http.MethodPost, "/rerun-stage-agent", strings.NewReader("{"))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	body := responseBody(t, rr)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
	if !strings.Contains(body, "bad request") {
		t.Fatalf("body = %q", body)
	}
}

func TestRerun_MissingTicketID(t *testing.T) {
	root := t.TempDir()
	srv := New(&fakeRunner{}, root)
	srv.RerunStageAgent = func(ticketID string, force bool, rows, cols uint16) (string, error) { return "", nil }
	mux := http.NewServeMux()
	mux.HandleFunc("/rerun-stage-agent", srv.handleRerunStageAgent)
	h := withCORS(mux)
	req := httptest.NewRequest(http.MethodPost, "/rerun-stage-agent", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	body := responseBody(t, rr)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
	if !strings.Contains(body, "ticket_id is required") {
		t.Fatalf("body = %q", body)
	}
}

func TestRerun_CallbackError(t *testing.T) {
	root := t.TempDir()
	srv := New(&fakeRunner{}, root)
	srv.RerunStageAgent = func(ticketID string, force bool, rows, cols uint16) (string, error) {
		return "", errors.New("no stage agent")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/rerun-stage-agent", srv.handleRerunStageAgent)
	h := withCORS(mux)
	req := httptest.NewRequest(http.MethodPost, "/rerun-stage-agent", strings.NewReader(`{"ticket_id":"TIC-5"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	body := responseBody(t, rr)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
	if !strings.Contains(body, "no stage agent") {
		t.Fatalf("body = %q", body)
	}
}

func TestRunCron_NoCallback(t *testing.T) {
	h := newTestHandler(t, &fakeRunner{}, t.TempDir())
	req := httptest.NewRequest(http.MethodPost, "/run-cron-agent", strings.NewReader(`{"name":"groomer"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rr.Code)
	}
}

func TestRunCron_BadJSON(t *testing.T) {
	root := t.TempDir()
	srv := New(&fakeRunner{}, root)
	srv.RunCronAgent = func(name string, rows, cols uint16) (string, error) { return "", nil }
	mux := http.NewServeMux()
	mux.HandleFunc("/run-cron-agent", srv.handleRunCronAgent)
	h := withCORS(mux)
	req := httptest.NewRequest(http.MethodPost, "/run-cron-agent", strings.NewReader("{"))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	body := responseBody(t, rr)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
	if !strings.Contains(body, "bad request") {
		t.Fatalf("body = %q", body)
	}
}

func TestRunCron_MissingName(t *testing.T) {
	root := t.TempDir()
	srv := New(&fakeRunner{}, root)
	srv.RunCronAgent = func(name string, rows, cols uint16) (string, error) { return "", nil }
	mux := http.NewServeMux()
	mux.HandleFunc("/run-cron-agent", srv.handleRunCronAgent)
	h := withCORS(mux)
	req := httptest.NewRequest(http.MethodPost, "/run-cron-agent", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	body := responseBody(t, rr)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
	if !strings.Contains(body, "name is required") {
		t.Fatalf("body = %q", body)
	}
}

func TestRunCron_CallbackError(t *testing.T) {
	root := t.TempDir()
	srv := New(&fakeRunner{}, root)
	srv.RunCronAgent = func(name string, rows, cols uint16) (string, error) { return "", errors.New("unknown cron") }
	mux := http.NewServeMux()
	mux.HandleFunc("/run-cron-agent", srv.handleRunCronAgent)
	h := withCORS(mux)
	req := httptest.NewRequest(http.MethodPost, "/run-cron-agent", strings.NewReader(`{"name":"groomer"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	body := responseBody(t, rr)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
	if !strings.Contains(body, "unknown cron") {
		t.Fatalf("body = %q", body)
	}
}

func TestRunCron_Conflict(t *testing.T) {
	root := t.TempDir()
	srv := New(&fakeRunner{}, root)
	srv.RunCronAgent = func(name string, rows, cols uint16) (string, error) { return "", ErrCronRunActive }
	mux := http.NewServeMux()
	mux.HandleFunc("/run-cron-agent", srv.handleRunCronAgent)
	h := withCORS(mux)
	req := httptest.NewRequest(http.MethodPost, "/run-cron-agent", strings.NewReader(`{"name":"groomer"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	body := responseBody(t, rr)
	if rr.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rr.Code)
	}
	if !strings.Contains(body, ErrCronRunActive.Error()) {
		t.Fatalf("body = %q", body)
	}
}

func TestRunCron_Success(t *testing.T) {
	root := t.TempDir()
	srv := New(&fakeRunner{}, root)
	srv.RunCronAgent = func(name string, rows, cols uint16) (string, error) {
		if name != "groomer" {
			t.Fatalf("name = %q, want groomer", name)
		}
		if rows != 40 || cols != 132 {
			t.Fatalf("rows/cols = %d/%d", rows, cols)
		}
		return ".cron-groomer-4", nil
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/run-cron-agent", srv.handleRunCronAgent)
	h := withCORS(mux)
	req := httptest.NewRequest(http.MethodPost, "/run-cron-agent", strings.NewReader(`{"name":"groomer","rows":40,"cols":132}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	var got spawnResponse
	if err := json.NewDecoder(rr.Result().Body).Decode(&got); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Session != ".cron-groomer-4" {
		t.Fatalf("session = %q", got.Session)
	}
}

func TestRerun_Success(t *testing.T) {
	root := t.TempDir()
	srv := New(&fakeRunner{}, root)
	var gotTicket string
	var gotForce bool
	var gotRows, gotCols uint16
	srv.RerunStageAgent = func(ticketID string, force bool, rows, cols uint16) (string, error) {
		gotTicket, gotForce, gotRows, gotCols = ticketID, force, rows, cols
		return "run-TIC-5", nil
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/rerun-stage-agent", srv.handleRerunStageAgent)
	h := withCORS(mux)
	req := httptest.NewRequest(http.MethodPost, "/rerun-stage-agent", strings.NewReader(`{"ticket_id":"TIC-5","rows":30,"cols":100}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	var out spawnResponse
	if err := json.NewDecoder(rr.Result().Body).Decode(&out); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if out.Session != "run-TIC-5" {
		t.Fatalf("session = %q", out.Session)
	}
	if gotTicket != "TIC-5" || gotForce || gotRows != 30 || gotCols != 100 {
		t.Fatalf("callback args = %q %t %d %d", gotTicket, gotForce, gotRows, gotCols)
	}
}

func TestRerun_ForceSuccess(t *testing.T) {
	root := t.TempDir()
	srv := New(&fakeRunner{}, root)
	var gotTicket string
	var gotForce bool
	var gotRows, gotCols uint16
	srv.RerunStageAgent = func(ticketID string, force bool, rows, cols uint16) (string, error) {
		gotTicket, gotForce, gotRows, gotCols = ticketID, force, rows, cols
		return "run-TIC-5", nil
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/rerun-stage-agent", srv.handleRerunStageAgent)
	h := withCORS(mux)
	req := httptest.NewRequest(http.MethodPost, "/rerun-stage-agent", strings.NewReader(`{"ticket_id":"TIC-5","force":true,"rows":30,"cols":100}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	var out spawnResponse
	if err := json.NewDecoder(rr.Result().Body).Decode(&out); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if out.Session != "run-TIC-5" {
		t.Fatalf("session = %q", out.Session)
	}
	if gotTicket != "TIC-5" || !gotForce || gotRows != 30 || gotCols != 100 {
		t.Fatalf("callback args = %q %t %d %d", gotTicket, gotForce, gotRows, gotCols)
	}
}

func TestWatchStatus_BadMethod(t *testing.T) {
	h := newTestHandler(t, &fakeRunner{}, t.TempDir())
	req := httptest.NewRequest(http.MethodPost, "/watch/status", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rr.Code)
	}
}

func TestWatchStatus_NoCallback(t *testing.T) {
	h := newTestHandler(t, &fakeRunner{}, t.TempDir())
	req := httptest.NewRequest(http.MethodGet, "/watch/status", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rr.Code)
	}
}

func TestWatchStatus_CallbackError(t *testing.T) {
	root := t.TempDir()
	srv := New(&fakeRunner{}, root)
	srv.WatchStatus = func() (WatchState, error) {
		return WatchState{}, errors.New("read failed")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/watch/status", srv.handleWatchStatus)
	h := withCORS(mux)
	req := httptest.NewRequest(http.MethodGet, "/watch/status", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	body := responseBody(t, rr)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rr.Code)
	}
	if !strings.Contains(body, "read failed") {
		t.Fatalf("body = %q", body)
	}
}

func TestWatchStatus_Success(t *testing.T) {
	root := t.TempDir()
	srv := New(&fakeRunner{}, root)
	srv.WatchStatus = func() (WatchState, error) {
		return WatchState{
			Paused:   true,
			PausedAt: mustTime(t, "2026-04-17T18:00:00Z"),
			Reason:   "release freeze",
		}, nil
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/watch/status", srv.handleWatchStatus)
	h := withCORS(mux)
	req := httptest.NewRequest(http.MethodGet, "/watch/status", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var got watchStateResponse
	if err := json.NewDecoder(rr.Result().Body).Decode(&got); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	want := watchStateResponse{
		Paused:   true,
		PausedAt: "2026-04-17T18:00:00Z",
		Reason:   "release freeze",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("watch status = %#v, want %#v", got, want)
	}
}

func TestWatchStatus_PausedWithWarning(t *testing.T) {
	root := t.TempDir()
	srv := New(&fakeRunner{}, root)
	srv.WatchStatus = func() (WatchState, error) {
		return WatchState{
			Paused:  true,
			Warning: "invalid character '}' looking for beginning of value",
		}, nil
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/watch/status", srv.handleWatchStatus)
	h := withCORS(mux)
	req := httptest.NewRequest(http.MethodGet, "/watch/status", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var got watchStateResponse
	if err := json.NewDecoder(rr.Result().Body).Decode(&got); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !got.Paused || got.Warning == "" {
		t.Fatalf("watch status = %#v, want paused warning response", got)
	}
}

func TestWatchPause_BadMethod(t *testing.T) {
	h := newTestHandler(t, &fakeRunner{}, t.TempDir())
	req := httptest.NewRequest(http.MethodGet, "/watch/pause", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rr.Code)
	}
}

func TestWatchPause_NoCallback(t *testing.T) {
	h := newTestHandler(t, &fakeRunner{}, t.TempDir())
	req := httptest.NewRequest(http.MethodPost, "/watch/pause", strings.NewReader(`{"reason":"freeze"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rr.Code)
	}
}

func TestWatchPause_BadJSON(t *testing.T) {
	root := t.TempDir()
	srv := New(&fakeRunner{}, root)
	srv.PauseWatch = func(reason string) (WatchState, error) { return WatchState{}, nil }
	mux := http.NewServeMux()
	mux.HandleFunc("/watch/pause", srv.handleWatchPause)
	h := withCORS(mux)
	req := httptest.NewRequest(http.MethodPost, "/watch/pause", strings.NewReader("{"))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	body := responseBody(t, rr)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
	if !strings.Contains(body, "bad request") {
		t.Fatalf("body = %q", body)
	}
}

func TestWatchPause_Success(t *testing.T) {
	root := t.TempDir()
	var gotReason string
	srv := New(&fakeRunner{}, root)
	srv.PauseWatch = func(reason string) (WatchState, error) {
		gotReason = reason
		return WatchState{
			Paused:   true,
			PausedAt: mustTime(t, "2026-04-17T18:10:00Z"),
			Reason:   reason,
		}, nil
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/watch/pause", srv.handleWatchPause)
	h := withCORS(mux)
	req := httptest.NewRequest(http.MethodPost, "/watch/pause", strings.NewReader(`{"reason":"release freeze"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if gotReason != "release freeze" {
		t.Fatalf("reason = %q, want %q", gotReason, "release freeze")
	}
}

func TestWatchResume_BadMethod(t *testing.T) {
	h := newTestHandler(t, &fakeRunner{}, t.TempDir())
	req := httptest.NewRequest(http.MethodGet, "/watch/resume", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rr.Code)
	}
}

func TestWatchResume_NoCallback(t *testing.T) {
	h := newTestHandler(t, &fakeRunner{}, t.TempDir())
	req := httptest.NewRequest(http.MethodPost, "/watch/resume", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rr.Code)
	}
}

func TestWatchResume_Success(t *testing.T) {
	root := t.TempDir()
	srv := New(&fakeRunner{}, root)
	srv.ResumeWatch = func() (WatchState, error) {
		return WatchState{}, nil
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/watch/resume", srv.handleWatchResume)
	h := withCORS(mux)
	req := httptest.NewRequest(http.MethodPost, "/watch/resume", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var got watchStateResponse
	if err := json.NewDecoder(rr.Result().Body).Decode(&got); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Paused {
		t.Fatalf("paused = true, want false")
	}
}

func TestTerminal_MissingSession(t *testing.T) {
	h := newTestHandler(t, &fakeRunner{}, t.TempDir())
	req := httptest.NewRequest(http.MethodGet, "/terminal/", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
}

func TestTerminal_SessionNotFound(t *testing.T) {
	h := newTestHandler(t, &fakeRunner{alive: map[string]bool{}}, t.TempDir())
	req := httptest.NewRequest(http.MethodGet, "/terminal/missing", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rr.Code)
	}
}

func TestParseSize(t *testing.T) {
	tests := []struct {
		name    string
		rows    string
		cols    string
		ok      bool
		wantRow uint16
		wantCol uint16
	}{
		{name: "empty"},
		{name: "nonnumeric", rows: "x", cols: "80"},
		{name: "zero", rows: "0", cols: "80"},
		{name: "too large", rows: "65536", cols: "80"},
		{name: "ok", rows: "24", cols: "80", ok: true, wantRow: 24, wantCol: 80},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows, cols, ok := parseSize(tt.rows, tt.cols)
			if ok != tt.ok || rows != tt.wantRow || cols != tt.wantCol {
				t.Fatalf("parseSize() = (%d, %d, %v), want (%d, %d, %v)", rows, cols, ok, tt.wantRow, tt.wantCol, tt.ok)
			}
		})
	}
}

func TestResizeMsg_Unmarshal(t *testing.T) {
	var msg resizeMsg
	if err := json.Unmarshal([]byte(`{"type":"resize","rows":40,"cols":132}`), &msg); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if msg.Type != "resize" || msg.Rows != 40 || msg.Cols != 132 {
		t.Fatalf("msg = %#v", msg)
	}

	if err := json.Unmarshal([]byte(`{"type":"noop","rows":40,"cols":132}`), &msg); err != nil {
		t.Fatalf("Unmarshal noop: %v", err)
	}
	if msg.Type == "resize" {
		t.Fatalf("expected noop message to be rejected: %#v", msg)
	}
}

func mustTime(t *testing.T, value string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("Parse(%q): %v", value, err)
	}
	return ts
}
