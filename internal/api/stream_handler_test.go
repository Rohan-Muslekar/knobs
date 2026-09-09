//go:build integration

package api_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Rohan-Muslekar/knobs/internal/api"
	"github.com/Rohan-Muslekar/knobs/internal/auth"
	"github.com/Rohan-Muslekar/knobs/internal/delivery"
	"github.com/Rohan-Muslekar/knobs/internal/store"
)

// streamTestApp bundles a router wired with a *live* Hub + Listener (a real
// dedicated pgx connection running LISTEN knobs_env against the test
// container), a session cookie for a seeded admin, the repo behind it, and
// the writable shutdown channel backing Deps.Shutdown (so a test can
// close() it to simulate app.Run's drain signal without needing a real
// OS signal). Distinct from seededRouter in projects_handlers_test.go,
// which builds a router with no Hub/Listener/shutdown wiring at all —
// insufficient here, since the whole point of this file is proving
// pg_notify -> Listener -> Hub -> SSE frame actually happens end to end,
// not mocking any leg of that chain.
func streamTestApp(t *testing.T) (router http.Handler, cookie string, repo *store.Repo, shutdown chan struct{}) {
	t.Helper()
	pool, url := migratedPoolAndURL(t)
	repo = store.New(pool)
	authr := auth.New("test-secret", false)
	hash, err := authr.Hash("pw")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if _, err := repo.CreateUser(t.Context(), repo.Pool(), "a@x.com", hash); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	hub := delivery.NewHub()
	loader := func(ctx context.Context, envID uuid.UUID) (delivery.Snapshot, bool) {
		env, err := repo.EnvironmentByID(ctx, repo.Pool(), envID)
		if err != nil {
			return delivery.Snapshot{}, false
		}
		cv, err := repo.CurrentVersion(ctx, repo.Pool(), envID)
		if err != nil {
			return delivery.Snapshot{}, false
		}
		cs, err := repo.GetOrInitSchema(ctx, repo.Pool(), env.ProjectID)
		if err != nil {
			return delivery.Snapshot{}, false
		}
		return delivery.BuildSnapshot(cv, cs.Definition), true
	}
	// The Listener owns its own dedicated, non-pooled connection (opened
	// from url, not pool) — same as app.Run wires it in production — so
	// this test genuinely exercises a real LISTEN session, not a pooled
	// query standing in for one.
	listener := delivery.NewListener(url, hub, loader)

	listenerCtx, cancelListener := context.WithCancel(context.Background())
	listenerDone := make(chan struct{})
	go func() {
		defer close(listenerDone)
		listener.Run(listenerCtx)
	}()
	t.Cleanup(func() {
		cancelListener()
		select {
		case <-listenerDone:
		case <-time.After(5 * time.Second):
			t.Error("listener did not stop within 5s of ctx cancellation")
		}
	})

	shutdown = make(chan struct{})

	// A short heartbeat interval so a test that happens to straddle one
	// doesn't need to wait tens of seconds; nothing here asserts on the
	// heartbeat itself, but the SSE frame reader below has to tolerate it
	// appearing, which it does by skipping ":"-prefixed comment lines.
	router = api.NewRouter(api.Deps{
		Repo: repo, Auth: authr, Hub: hub,
		StreamHeartbeat: 300 * time.Millisecond,
		Shutdown:        shutdown,
	})

	rec := post(router, "/v1/auth/login", `{"email":"a@x.com","password":"pw"}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("login failed: %d", rec.Code)
	}
	cookie = rec.Result().Cookies()[0].Value
	return router, cookie, repo, shutdown
}

// seedStreamFixture creates a project + schema + environment + an initial
// values write (version 1) + an API key for that environment, via the
// existing in-process httptest.ResponseRecorder helpers (fast, no
// network). Shared by every test in this file that needs a real
// environment to open a stream against.
func seedStreamFixture(t *testing.T, router http.Handler, cookie string) (envID, key string) {
	t.Helper()
	projRec := post(router, "/v1/projects", `{"name":"Acme","slug":"acme"}`, cookie)
	if projRec.Code != http.StatusCreated {
		t.Fatalf("create project = %d, want 201, body=%s", projRec.Code, projRec.Body.String())
	}
	var proj map[string]any
	_ = json.NewDecoder(projRec.Body).Decode(&proj)
	projectID, _ := proj["id"].(string)

	schemaBody := `{"fields":[{"name":"maxRetries","type":"int","required":true,"max":10}]}`
	if rec := put(router, "/v1/projects/"+projectID+"/schema", schemaBody, cookie); rec.Code != http.StatusOK {
		t.Fatalf("put schema = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	envRec := post(router, "/v1/projects/"+projectID+"/environments", `{"name":"staging"}`, cookie)
	if envRec.Code != http.StatusCreated {
		t.Fatalf("create env = %d, want 201, body=%s", envRec.Code, envRec.Body.String())
	}
	var env map[string]any
	_ = json.NewDecoder(envRec.Body).Decode(&env)
	envID, _ = env["id"].(string)

	// Version 1, before any stream is open.
	if rec := put(router, "/v1/environments/"+envID+"/values", `{"values":{"maxRetries":3}}`, cookie); rec.Code != http.StatusOK {
		t.Fatalf("put values v1 = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	keyRec := post(router, "/v1/environments/"+envID+"/api-keys", `{"name":"sdk"}`, cookie)
	if keyRec.Code != http.StatusCreated {
		t.Fatalf("create key = %d, want 201, body=%s", keyRec.Code, keyRec.Body.String())
	}
	var keyBody map[string]any
	_ = json.NewDecoder(keyRec.Body).Decode(&keyBody)
	key, _ = keyBody["key"].(string)
	if key == "" {
		t.Fatal("no plaintext key returned")
	}
	return envID, key
}

// sseFrames streams data: frames from body onto a channel in the
// background, skipping blank lines and ":"-prefixed comments (heartbeats).
// The channel closes when the body is exhausted or errors (e.g. the caller
// closed the response, the handler returned on its own, or the test
// server shut down).
func sseFrames(body *http.Response) <-chan map[string]any {
	frames := make(chan map[string]any, 16)
	go func() {
		defer close(frames)
		reader := bufio.NewReader(body.Body)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\n")
			if line == "" || strings.HasPrefix(line, ":") {
				continue
			}
			payload, ok := strings.CutPrefix(line, "data: ")
			if !ok {
				continue
			}
			var m map[string]any
			if err := json.Unmarshal([]byte(payload), &m); err != nil {
				continue
			}
			frames <- m
		}
	}()
	return frames
}

// waitFrame reads the next frame from frames, failing the test if none
// arrives within timeout. Bounded so a broken fan-out fails the test
// quickly instead of hanging the suite.
func waitFrame(t *testing.T, frames <-chan map[string]any, timeout time.Duration) map[string]any {
	t.Helper()
	select {
	case f, ok := <-frames:
		if !ok {
			t.Fatal("stream closed before expected frame arrived")
		}
		return f
	case <-time.After(timeout):
		t.Fatal("timed out waiting for a stream frame")
		return nil
	}
}

// waitForVersion reads frames until one with exactly the given version
// arrives, discarding any with a lower version along the way, and fails if
// a version greater than want arrives first (a real skip/bug) or if want
// never shows up within timeout.
//
// Discarding lower versions is deliberate, not a workaround for a broken
// test: the Listener's loader always re-reads whatever the "current"
// version happens to be at the moment it processes a notification (see
// listener.go's parsePayload doc), rather than trusting the version named
// in the payload. If a notification's processing is delayed — e.g. by
// scheduler or Docker network jitter, which a slow-write-timeout test
// exacerbates by deliberately idling the connection — it can end up
// publishing a stale duplicate of a version already delivered. That's
// harmless for a real client (which only ever tracks the max version
// seen and would no-op on a repeat), so tests that assert a specific
// later version should tolerate a stale repeat rather than treat it as a
// failure the way they'd treat an out-of-order skip.
func waitForVersion(t *testing.T, frames <-chan map[string]any, timeout time.Duration, want int) map[string]any {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case f, ok := <-frames:
			if !ok {
				t.Fatal("stream closed before expected version arrived")
			}
			v, _ := f["version"].(float64)
			switch {
			case int(v) == want:
				return f
			case int(v) > want:
				t.Fatalf("frame version = %v, want %d (skipped ahead unexpectedly)", v, want)
			}
			// v < want: a stale/duplicate replay of an earlier version;
			// discard and keep waiting for the real one.
		case <-deadline:
			t.Fatalf("timed out waiting for version %d", want)
			return nil
		}
	}
}

// assertNoFrame fails the test if a frame arrives within window. Used to
// prove `since=<current>` suppresses the immediate snapshot.
func assertNoFrame(t *testing.T, frames <-chan map[string]any, window time.Duration) {
	t.Helper()
	select {
	case f, ok := <-frames:
		if ok {
			t.Fatalf("received unexpected frame %+v, want none", f)
		}
	case <-time.After(window):
		// expected: nothing arrived.
	}
}

// TestStreamFanOut is the end-to-end proof that a PUT values write reaches
// an open SSE stream via pg_notify -> Listener (dedicated LISTEN
// connection) -> Hub -> handleStream, and that `since` correctly gates the
// immediate on-connect snapshot. All waits below are bounded (waitFrame /
// assertNoFrame timeouts), so a broken fan-out fails fast rather than
// hanging the suite.
func TestStreamFanOut(t *testing.T) {
	router, cookie, _, _ := streamTestApp(t)
	envID, key := seedStreamFixture(t, router, cookie)

	ts := httptest.NewServer(router)
	defer ts.Close()

	// A bad key must be rejected with 401 before any streaming begins —
	// checked against the real server (not a recorder), since that's what
	// actually exercises the header-write-order guarantee: writeErr must
	// run instead of the SSE headers, entirely inside apiKeyGuard, before
	// handleStream is ever reached.
	badReq, _ := http.NewRequest(http.MethodGet, ts.URL+"/v1/stream", nil)
	badReq.Header.Set("Authorization", "Bearer knobs_not-a-real-key")
	badResp, err := http.DefaultClient.Do(badReq)
	if err != nil {
		t.Fatalf("bad key request: %v", err)
	}
	badResp.Body.Close()
	if badResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad key stream = %d, want 401", badResp.StatusCode)
	}

	// Open the real stream with since=0: the current version (1) is newer,
	// so it must arrive immediately.
	streamCtx, cancelStream := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancelStream()
	req, _ := http.NewRequestWithContext(streamCtx, http.MethodGet, ts.URL+"/v1/stream?since=0", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stream status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-cache" {
		t.Fatalf("Cache-Control = %q, want no-cache", cc)
	}
	if conn := resp.Header.Get("Connection"); conn != "keep-alive" {
		t.Fatalf("Connection = %q, want keep-alive", conn)
	}

	frames := sseFrames(resp)

	initial := waitFrame(t, frames, 5*time.Second)
	if v, _ := initial["version"].(float64); v != 1 {
		t.Fatalf("initial frame version = %v, want 1", initial["version"])
	}
	initialValues, _ := initial["values"].(map[string]any)
	if mr, _ := initialValues["maxRetries"].(float64); mr != 3 {
		t.Fatalf("initial frame maxRetries = %v, want 3", initialValues["maxRetries"])
	}

	// The write path below is the actual system under test for fan-out:
	// PUT values -> WithTx -> pg_notify (delivered on commit) -> the
	// Listener's dedicated LISTEN connection -> Hub.Publish -> this stream.
	if rec := put(router, "/v1/environments/"+envID+"/values", `{"values":{"maxRetries":7}}`, cookie); rec.Code != http.StatusOK {
		t.Fatalf("put values v2 = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	updated := waitForVersion(t, frames, 10*time.Second, 2)
	updatedValues, _ := updated["values"].(map[string]any)
	if mr, _ := updatedValues["maxRetries"].(float64); mr != 7 {
		t.Fatalf("updated frame maxRetries = %v, want 7 (fan-out did not deliver the new value)", updatedValues["maxRetries"])
	}
	resp.Body.Close()

	// Second connection: since=<current version, 2> must NOT get an
	// immediate snapshot (nothing is newer yet), but a subsequent write
	// still reaches it.
	req2, _ := http.NewRequestWithContext(streamCtx, http.MethodGet, ts.URL+"/v1/stream?since=2", nil)
	req2.Header.Set("Authorization", "Bearer "+key)
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("open second stream: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("second stream status = %d, want 200", resp2.StatusCode)
	}
	frames2 := sseFrames(resp2)

	assertNoFrame(t, frames2, 1*time.Second)

	if rec := put(router, "/v1/environments/"+envID+"/values", `{"values":{"maxRetries":9}}`, cookie); rec.Code != http.StatusOK {
		t.Fatalf("put values v3 = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	third := waitForVersion(t, frames2, 10*time.Second, 3)

	// Confirm the fed-back schemaHash matches GET /v1/snapshot's for the
	// same state, proving the stream uses the same helper rather than a
	// divergent computation.
	snapRec := bearer(router, "/v1/snapshot", key)
	if snapRec.Code != http.StatusOK {
		t.Fatalf("snapshot = %d, want 200, body=%s", snapRec.Code, snapRec.Body.String())
	}
	var snap map[string]any
	_ = json.NewDecoder(snapRec.Body).Decode(&snap)
	if snap["schemaHash"] != third["schemaHash"] {
		t.Fatalf("stream schemaHash %v != snapshot schemaHash %v", third["schemaHash"], snap["schemaHash"])
	}
}

// TestStreamSurvivesShortWriteTimeout is a regression test for
// http.Server.WriteTimeout killing every SSE stream partway through: that
// timeout sets the connection's write deadline once, at request start, and
// never refreshes it — so without handleStream clearing it via
// http.NewResponseController.SetWriteDeadline, any write attempted after
// the deadline (the periodic heartbeat, or a fan-out frame) fails with an
// i/o timeout and the handler returns, closing the stream out from under
// the client deterministically once WriteTimeout elapses, no matter how
// active the connection is.
//
// This test pins WriteTimeout to 1s (via a manually-configured
// httptest.Server, since the plain httptest.NewServer sets no timeouts at
// all) and a 300ms heartbeat, then waits 1.5s — well past the deadline,
// during which several heartbeat writes must have been attempted — before
// triggering a write. Without the fix in handleStream, those heartbeat
// writes fail once the deadline passes, the handler returns, and the
// subsequent PUT's fan-out frame is never delivered (frames closes,
// waitFrame fails via "stream closed"). With the fix, the deadline is
// cleared entirely, so the wait is a no-op and the frame arrives normally.
func TestStreamSurvivesShortWriteTimeout(t *testing.T) {
	router, cookie, _, _ := streamTestApp(t)
	envID, key := seedStreamFixture(t, router, cookie)

	ts := httptest.NewUnstartedServer(router)
	ts.Config.WriteTimeout = 1 * time.Second
	ts.Start()
	defer ts.Close()

	streamCtx, cancelStream := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancelStream()
	req, _ := http.NewRequestWithContext(streamCtx, http.MethodGet, ts.URL+"/v1/stream?since=0", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stream status = %d, want 200", resp.StatusCode)
	}
	frames := sseFrames(resp)

	initial := waitFrame(t, frames, 5*time.Second)
	if v, _ := initial["version"].(float64); v != 1 {
		t.Fatalf("initial frame version = %v, want 1", initial["version"])
	}

	// Sit past the 1s WriteTimeout, during which the 300ms heartbeat
	// ticker fires several times. Each of those is a write on the
	// connection; if handleStream hadn't cleared the deadline, the first
	// one attempted after the deadline passes fails with i/o timeout and
	// ends the stream right here.
	time.Sleep(1500 * time.Millisecond)

	if rec := put(router, "/v1/environments/"+envID+"/values", `{"values":{"maxRetries":7}}`, cookie); rec.Code != http.StatusOK {
		t.Fatalf("put values v2 = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	// waitForVersion (rather than a plain single waitFrame) tolerates a
	// possible stale duplicate of version 1 landing here too: the sleep
	// above widens the window for the benign late-notification replay
	// documented on waitForVersion, on top of what it's actually proving
	// (survival past WriteTimeout).
	waitForVersion(t, frames, 10*time.Second, 2)
}

// TestStreamHeartbeat proves handleStream actually emits a `: heartbeat`
// SSE comment while idle, using streamTestApp's injected 300ms
// StreamHeartbeat so the test doesn't wait out the real 25s default.
// It opens with since=<current version> so the immediate on-connect
// snapshot is suppressed (see TestStreamFanOut's second connection for the
// same suppression) — but that alone isn't enough to make the heartbeat
// the very first thing off the wire: the Listener can still deliver a
// stale-duplicate `data:` frame for a version <= since (the same benign
// late-notification replay waitForVersion above tolerates, e.g. from the
// seed write in seedStreamFixture being processed after this stream
// subscribes). So this test discards any leading data frames at or below
// since exactly like waitForVersion discards stale versions, and only
// fails if a *newer* frame or a non-comment, non-data line shows up before
// the heartbeat, or if no heartbeat arrives within the bounded timeout.
func TestStreamHeartbeat(t *testing.T) {
	router, cookie, _, _ := streamTestApp(t)
	_, key := seedStreamFixture(t, router, cookie)

	ts := httptest.NewServer(router)
	defer ts.Close()

	streamCtx, cancelStream := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancelStream()
	// seedStreamFixture leaves the environment at version 1; since=1 equals
	// the current version, so no immediate snapshot is sent (per
	// handleStream's `cv.Version > since` gate).
	const since = 1
	req, _ := http.NewRequestWithContext(streamCtx, http.MethodGet, ts.URL+"/v1/stream?since=1", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stream status = %d, want 200", resp.StatusCode)
	}

	type lineResult struct {
		line string
		err  error
	}
	lines := make(chan lineResult, 16)
	go func() {
		reader := bufio.NewReader(resp.Body)
		for {
			line, err := reader.ReadString('\n')
			lines <- lineResult{line: strings.TrimRight(line, "\n"), err: err}
			if err != nil {
				return
			}
		}
	}()

	deadline := time.After(5 * time.Second)
	for {
		select {
		case res := <-lines:
			if res.err != nil {
				t.Fatalf("read line: %v", res.err)
			}
			switch {
			case res.line == "":
				// Blank line separating SSE events; keep reading.
				continue
			case strings.HasPrefix(res.line, ":"):
				// The heartbeat comment itself — this is what the test is
				// actually proving. Any content after the leading ":" (the
				// handler writes ": heartbeat") is accepted; the ":" is
				// what makes it a comment per the SSE spec.
				return
			default:
				payload, ok := strings.CutPrefix(res.line, "data: ")
				if !ok {
					t.Fatalf("unexpected line before heartbeat: %q", res.line)
				}
				var m map[string]any
				if err := json.Unmarshal([]byte(payload), &m); err != nil {
					t.Fatalf("unparseable data frame before heartbeat: %q: %v", res.line, err)
				}
				if v, _ := m["version"].(float64); int(v) > since {
					t.Fatalf("data frame with version %v (> since %d) arrived before the heartbeat", m["version"], since)
				}
				// version <= since: a benign stale-duplicate replay, same
				// as waitForVersion discards above. Keep reading for the
				// heartbeat.
			}
		case <-deadline:
			t.Fatal("timed out waiting for the heartbeat comment")
		}
	}
}

// TestStreamShutdownSignalDrainsConnection proves that closing Deps.Shutdown
// (what app.Run does, ahead of calling srv.Shutdown, on SIGINT/SIGTERM)
// makes an open handleStream connection return promptly on its own — the
// mechanism that lets graceful shutdown actually drain active SSE streams
// instead of always burning its full timeout waiting on connections
// srv.Shutdown itself never asks to stop.
//
// Bounded: the drain loop below runs against a single generous 3s overall
// deadline, so a regression (handleStream not observing the shutdown
// channel) fails this test in 3s rather than hanging.
func TestStreamShutdownSignalDrainsConnection(t *testing.T) {
	router, cookie, _, shutdown := streamTestApp(t)
	_, key := seedStreamFixture(t, router, cookie)

	ts := httptest.NewServer(router)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/v1/stream?since=0", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stream status = %d, want 200", resp.StatusCode)
	}
	frames := sseFrames(resp)

	// Consume the initial snapshot first, so the assertion below is
	// clearly about the shutdown signal closing an established, flowing
	// stream — not about a connection that never got going.
	_ = waitFrame(t, frames, 5*time.Second)

	close(shutdown)

	// handleStream's select has no priority between its Hub-channel case
	// and its shutdown case: if a late, already-queued Publish (the same
	// benign stale-replay described on waitForVersion — a notification
	// for the earlier seed write, processed late) happens to be pending
	// at the exact moment shutdown fires, select may fairly choose to
	// deliver that one frame before the next iteration picks the
	// shutdown case. That's a legitimate outcome of a select-based
	// design, not a failure to drain — so this loop tolerates any number
	// of frames arriving before the close, and only fails if the stream
	// never actually closes within the deadline.
	deadline := time.After(3 * time.Second)
	for {
		select {
		case _, ok := <-frames:
			if !ok {
				return // success: the handler returned, body closed.
			}
			// A frame delivered concurrently with the shutdown signal;
			// keep waiting for the actual close.
		case <-deadline:
			t.Fatal("stream did not close within 3s of the shutdown signal — handleStream is not observing Deps.Shutdown")
		}
	}
}
