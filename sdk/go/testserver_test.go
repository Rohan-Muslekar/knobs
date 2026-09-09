package knobs

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
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

	mu              sync.Mutex
	snapshot        Snapshot
	notFound        bool
	lastAuth        string
	unmatchedPath   string
	lastStreamQuery string
	snapshotHits    int

	// streamConnReady receives a channel for every new /v1/stream connection,
	// as soon as that connection's handler goroutine is up and blocked
	// waiting for frames to write. A test drains one entry per connection it
	// expects (via waitForStreamConn) and then sends raw SSE bytes into that
	// channel (via sendFrame) to drive the connection deterministically —
	// nothing is written to the wire until the test says so.
	streamConnReady chan chan string
}

func newTestServer(prefix string) *testServer {
	ts := &testServer{
		streamConnReady: make(chan chan string, 8),
	}
	mux := http.NewServeMux()
	mux.HandleFunc(prefix+"/v1/snapshot", ts.handleSnapshot)
	mux.HandleFunc(prefix+"/v1/stream", ts.handleStream)
	mux.HandleFunc("/", ts.handleUnmatched)
	ts.Server = httptest.NewServer(mux)
	return ts
}

// handleStream serves a controllable SSE connection: it writes+flushes
// nothing on its own, just registers a per-connection channel and relays
// whatever raw bytes a test sends into it, until the request's context is
// cancelled (the client disconnected, e.g. via Close) or the channel is
// closed.
func (ts *testServer) handleStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "stream: ResponseWriter doesn't support flushing", http.StatusInternalServerError)
		return
	}

	ts.mu.Lock()
	ts.lastAuth = r.Header.Get("Authorization")
	ts.lastStreamQuery = r.URL.RawQuery
	ts.mu.Unlock()

	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ch := make(chan string)
	select {
	case ts.streamConnReady <- ch:
	case <-r.Context().Done():
		return
	}

	for {
		select {
		case <-r.Context().Done():
			return
		case chunk, ok := <-ch:
			if !ok {
				return
			}
			if _, err := w.Write([]byte(chunk)); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// waitForStreamConn blocks until a client has connected to /v1/stream and
// returns the channel a test can use (via sendFrame) to write raw SSE bytes
// to that specific connection. Fails the test if no connection shows up
// within a bounded window, so a wiring bug shows up as a fast test failure
// rather than a hang.
func (ts *testServer) waitForStreamConn(t *testing.T) chan string {
	t.Helper()
	select {
	case ch := <-ts.streamConnReady:
		return ch
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for a /v1/stream connection")
		return nil
	}
}

// sendFrame writes raw bytes (typically one SSE frame, or a fragment of
// one) to a stream connection obtained from waitForStreamConn, blocking
// until the connection's handler goroutine has picked it up. Fails the test
// rather than hanging if that doesn't happen promptly.
func sendFrame(t *testing.T, ch chan string, data string) {
	t.Helper()
	select {
	case ch <- data:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out sending SSE frame — no active stream reader?")
	}
}

func (ts *testServer) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	ts.mu.Lock()
	ts.lastAuth = r.Header.Get("Authorization")
	ts.snapshotHits++
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

func (ts *testServer) streamQuery() string {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.lastStreamQuery
}

func (ts *testServer) snapshotHitCount() int {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.snapshotHits
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
