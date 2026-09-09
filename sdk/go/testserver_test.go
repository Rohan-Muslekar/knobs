package knobs

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
)

// testServer is a controllable fake of the delivery API's snapshot endpoint.
// It serves GET {prefix}/v1/snapshot with a settable Snapshot (JSON) or a 404,
// and records the Authorization header it saw so tests can assert the Bearer
// key was actually sent. A prefix (e.g. "/knobs") lets tests exercise a
// path-prefixed endpoint; anything that misses the exact prefixed route falls
// through to a catch-all so a broken URL-join shows up as a recorded miss
// rather than a plain 404 that could be mistaken for the "no values yet" case.
type testServer struct {
	*httptest.Server

	mu            sync.Mutex
	snapshot      Snapshot
	notFound      bool
	lastAuth      string
	unmatchedPath string
}

func newTestServer(prefix string) *testServer {
	ts := &testServer{}
	mux := http.NewServeMux()
	mux.HandleFunc(prefix+"/v1/snapshot", ts.handleSnapshot)
	mux.HandleFunc("/", ts.handleUnmatched)
	ts.Server = httptest.NewServer(mux)
	return ts
}

func (ts *testServer) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	ts.mu.Lock()
	ts.lastAuth = r.Header.Get("Authorization")
	notFound := ts.notFound
	snap := ts.snapshot
	ts.mu.Unlock()

	if notFound {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(snap)
}

// handleUnmatched catches any request that didn't hit the exact prefixed
// route above, e.g. because fetchSnapshot dropped the endpoint's path prefix.
func (ts *testServer) handleUnmatched(w http.ResponseWriter, r *http.Request) {
	ts.mu.Lock()
	ts.unmatchedPath = r.URL.Path
	ts.mu.Unlock()
	w.WriteHeader(http.StatusNotFound)
}

func (ts *testServer) setSnapshot(snap Snapshot) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.snapshot = snap
	ts.notFound = false
}

func (ts *testServer) setNotFound() {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.notFound = true
}

func (ts *testServer) authHeader() string {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.lastAuth
}

func (ts *testServer) unmatched() string {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.unmatchedPath
}

// testLogHandler is a slog.Handler that just records records in memory, so
// tests can assert on warnings without parsing log output.
type testLogHandler struct {
	mu   sync.Mutex
	recs []slog.Record
}

func newTestLogger() (*slog.Logger, *testLogHandler) {
	h := &testLogHandler{}
	return slog.New(h), h
}

func (h *testLogHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *testLogHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.recs = append(h.recs, r.Clone())
	return nil
}

func (h *testLogHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *testLogHandler) WithGroup(string) slog.Handler      { return h }

func (h *testLogHandler) warnCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, r := range h.recs {
		if r.Level == slog.LevelWarn {
			n++
		}
	}
	return n
}

func (h *testLogHandler) hasWarnContaining(substr string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, r := range h.recs {
		if r.Level == slog.LevelWarn && strings.Contains(r.Message, substr) {
			return true
		}
	}
	return false
}
