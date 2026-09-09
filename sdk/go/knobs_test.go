package knobs

import (
	"context"
	"testing"
	"time"
)

// noPoll is a PollInterval long enough that the poll fallback ticker never
// fires during these tests, so the only live-update path exercised is the
// stream — keeping the revision-gate assertions unambiguous about which
// path applied a snapshot.
const noPoll = time.Hour

func testContext(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestReadyLoadsSnapshotForGetAndGetAll(t *testing.T) {
	srv := newTestServer("")
	defer srv.Close()
	srv.setSnapshot(Snapshot{
		Version:    3,
		Revision:   10,
		SchemaHash: "abc123",
		Values:     map[string]any{"foo": "bar", "limit": float64(42)},
	})

	c := New(Options{Endpoint: srv.URL, APIKey: "test-key"})
	if err := c.Ready(testContext(t)); err != nil {
		t.Fatalf("Ready() error = %v", err)
	}
	defer c.Close()

	if v, ok := c.Get("foo"); !ok || v != "bar" {
		t.Errorf("Get(%q) = (%v, %v), want (\"bar\", true)", "foo", v, ok)
	}
	if v, ok := c.Get("limit"); !ok || v != float64(42) {
		t.Errorf("Get(%q) = (%v, %v), want (42, true)", "limit", v, ok)
	}
	if _, ok := c.Get("missing"); ok {
		t.Errorf("Get(%q) ok = true, want false", "missing")
	}

	all := c.GetAll()
	if len(all) != 2 || all["foo"] != "bar" || all["limit"] != float64(42) {
		t.Errorf("GetAll() = %v, want {foo:bar, limit:42}", all)
	}
}

func TestGetAllReturnsIsolatedCopy(t *testing.T) {
	srv := newTestServer("")
	defer srv.Close()
	srv.setSnapshot(Snapshot{
		Revision: 1,
		Values:   map[string]any{"foo": "bar"},
	})

	c := New(Options{Endpoint: srv.URL, APIKey: "test-key"})
	if err := c.Ready(testContext(t)); err != nil {
		t.Fatalf("Ready() error = %v", err)
	}
	defer c.Close()

	first := c.GetAll()
	first["foo"] = "mutated"
	first["extra"] = "should not leak"

	second := c.GetAll()
	if second["foo"] != "bar" {
		t.Errorf("second GetAll()[\"foo\"] = %v, want %q (mutating the first copy leaked into internal state)", second["foo"], "bar")
	}
	if _, ok := second["extra"]; ok {
		t.Error("second GetAll() contains a key added only to a previously returned copy")
	}

	if v, _ := c.Get("foo"); v != "bar" {
		t.Errorf("Get(\"foo\") = %v after mutating a GetAll() copy, want %q", v, "bar")
	}
}

func TestNotFoundSnapshotIsEmptyNotError(t *testing.T) {
	srv := newTestServer("")
	defer srv.Close()
	srv.setNotFound()

	c := New(Options{Endpoint: srv.URL, APIKey: "test-key"})
	if err := c.Ready(testContext(t)); err != nil {
		t.Fatalf("Ready() error = %v, want nil on 404", err)
	}
	defer c.Close()

	all := c.GetAll()
	if len(all) != 0 {
		t.Errorf("GetAll() = %v, want empty map", all)
	}
	if _, ok := c.Get("anything"); ok {
		t.Error("Get() ok = true on an empty snapshot, want false")
	}
}

func TestExpectedSchemaHashMismatchWarnsOnce(t *testing.T) {
	srv := newTestServer("")
	defer srv.Close()
	srv.setSnapshot(Snapshot{
		Revision:   1,
		SchemaHash: "server-hash",
		Values:     map[string]any{"foo": "bar"},
	})

	logger, handler := newTestLogger()
	c := New(Options{
		Endpoint:           srv.URL,
		APIKey:             "test-key",
		ExpectedSchemaHash: "generated-hash",
		Logger:             logger,
	})
	if err := c.Ready(testContext(t)); err != nil {
		t.Fatalf("Ready() error = %v", err)
	}
	defer c.Close()

	if got := handler.warnCount(); got != 1 {
		t.Errorf("warnCount() = %d, want 1", got)
	}
	if !handler.hasWarnContaining("schemaHash") {
		t.Error("expected a warning mentioning schemaHash")
	}
}

func TestExpectedSchemaHashMatchDoesNotWarn(t *testing.T) {
	srv := newTestServer("")
	defer srv.Close()
	srv.setSnapshot(Snapshot{
		Revision:   1,
		SchemaHash: "same-hash",
		Values:     map[string]any{"foo": "bar"},
	})

	logger, handler := newTestLogger()
	c := New(Options{
		Endpoint:           srv.URL,
		APIKey:             "test-key",
		ExpectedSchemaHash: "same-hash",
		Logger:             logger,
	})
	if err := c.Ready(testContext(t)); err != nil {
		t.Fatalf("Ready() error = %v", err)
	}
	defer c.Close()

	if got := handler.warnCount(); got != 0 {
		t.Errorf("warnCount() = %d, want 0 when hashes match", got)
	}
}

func TestNotFoundWithExpectedSchemaHashDoesNotWarn(t *testing.T) {
	srv := newTestServer("")
	defer srv.Close()
	srv.setNotFound()

	logger, handler := newTestLogger()
	c := New(Options{
		Endpoint:           srv.URL,
		APIKey:             "test-key",
		ExpectedSchemaHash: "generated-hash",
		Logger:             logger,
	})
	if err := c.Ready(testContext(t)); err != nil {
		t.Fatalf("Ready() error = %v", err)
	}
	defer c.Close()

	if got := handler.warnCount(); got != 0 {
		t.Errorf("warnCount() = %d, want 0 on an empty (404) snapshot", got)
	}
}

func TestReadySendsBearerAuthHeader(t *testing.T) {
	srv := newTestServer("")
	defer srv.Close()
	srv.setSnapshot(Snapshot{Revision: 1, Values: map[string]any{}})

	c := New(Options{Endpoint: srv.URL, APIKey: "super-secret-key"})
	if err := c.Ready(testContext(t)); err != nil {
		t.Fatalf("Ready() error = %v", err)
	}
	defer c.Close()

	want := "Bearer super-secret-key"
	if got := srv.authHeader(); got != want {
		t.Errorf("Authorization header = %q, want %q", got, want)
	}
}

func TestReadyPreservesEndpointPathPrefix(t *testing.T) {
	srv := newTestServer("/knobs")
	defer srv.Close()
	srv.setSnapshot(Snapshot{
		Revision: 1,
		Values:   map[string]any{"foo": "bar"},
	})

	c := New(Options{Endpoint: srv.URL + "/knobs", APIKey: "test-key"})
	if err := c.Ready(testContext(t)); err != nil {
		t.Fatalf("Ready() error = %v", err)
	}
	defer c.Close()

	if v, ok := c.Get("foo"); !ok || v != "bar" {
		t.Errorf("Get(\"foo\") = (%v, %v), want (\"bar\", true)", v, ok)
	}
	if unmatched := srv.unmatched(); unmatched != "" {
		t.Errorf("request hit unexpected path %q — the endpoint's /knobs prefix was dropped", unmatched)
	}
}

func TestCloseBeforeReadyIsSafe(t *testing.T) {
	c := New(Options{Endpoint: "http://127.0.0.1:0", APIKey: "test-key"})
	c.Close()
	c.Close() // idempotent
}

func TestStreamedHigherRevisionUpdatesGetAllAndFiresOnChange(t *testing.T) {
	srv := newTestServer("")
	defer srv.Close()
	srv.setSnapshot(Snapshot{Version: 1, Revision: 1, Values: map[string]any{"foo": "bar"}})

	c := New(Options{Endpoint: srv.URL, APIKey: "test-key", PollInterval: noPoll})
	if err := c.Ready(testContext(t)); err != nil {
		t.Fatalf("Ready() error = %v", err)
	}
	defer c.Close()

	changed := make(chan map[string]any, 1)
	unsub := c.OnChange(func(values map[string]any) { changed <- values })
	defer unsub()

	ch := srv.waitForStreamConn(t)
	sendFrame(t, ch, `data: {"version":2,"revision":5,"schemaHash":"h","values":{"foo":"baz"}}`+"\n\n")

	select {
	case values := <-changed:
		if values["foo"] != "baz" {
			t.Fatalf("OnChange values = %v, want foo=baz", values)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for OnChange on a higher-revision frame")
	}

	if all := c.GetAll(); all["foo"] != "baz" {
		t.Fatalf("GetAll()[\"foo\"] = %v, want %q", all["foo"], "baz")
	}
}

func TestStreamedEqualOrLowerRevisionIsIgnored(t *testing.T) {
	srv := newTestServer("")
	defer srv.Close()
	srv.setSnapshot(Snapshot{Version: 1, Revision: 10, Values: map[string]any{"foo": "bar"}})

	c := New(Options{Endpoint: srv.URL, APIKey: "test-key", PollInterval: noPoll})
	if err := c.Ready(testContext(t)); err != nil {
		t.Fatalf("Ready() error = %v", err)
	}
	defer c.Close()

	fired := make(chan struct{}, 1)
	unsub := c.OnChange(func(map[string]any) { fired <- struct{}{} })
	defer unsub()

	ch := srv.waitForStreamConn(t)
	// Equal revision: a stale replay, must be ignored.
	sendFrame(t, ch, `data: {"version":2,"revision":10,"schemaHash":"h","values":{"foo":"equal"}}`+"\n\n")
	// Lower revision: also a stale replay, must be ignored.
	sendFrame(t, ch, `data: {"version":2,"revision":5,"schemaHash":"h","values":{"foo":"lower"}}`+"\n\n")

	select {
	case <-fired:
		t.Fatal("OnChange fired for an equal/lower-revision frame")
	case <-time.After(300 * time.Millisecond):
	}

	if all := c.GetAll(); all["foo"] != "bar" {
		t.Fatalf("GetAll()[\"foo\"] = %v, want unchanged %q", all["foo"], "bar")
	}
}

func TestStreamedRollbackHigherRevisionLowerVersionApplies(t *testing.T) {
	srv := newTestServer("")
	defer srv.Close()
	srv.setSnapshot(Snapshot{Version: 5, Revision: 10, Values: map[string]any{"foo": "v5"}})

	c := New(Options{Endpoint: srv.URL, APIKey: "test-key", PollInterval: noPoll})
	if err := c.Ready(testContext(t)); err != nil {
		t.Fatalf("Ready() error = %v", err)
	}
	defer c.Close()

	changed := make(chan map[string]any, 1)
	unsub := c.OnChange(func(values map[string]any) { changed <- values })
	defer unsub()

	ch := srv.waitForStreamConn(t)
	// Rollback: Version went DOWN (5 -> 3) but Revision went UP (10 -> 11) —
	// must still apply, since revision (not version) is the dedup axis.
	sendFrame(t, ch, `data: {"version":3,"revision":11,"schemaHash":"h","values":{"foo":"rolled-back"}}`+"\n\n")

	select {
	case values := <-changed:
		if values["foo"] != "rolled-back" {
			t.Fatalf("OnChange values = %v, want foo=rolled-back", values)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for OnChange on a rollback frame")
	}

	if all := c.GetAll(); all["foo"] != "rolled-back" {
		t.Fatalf("GetAll()[\"foo\"] = %v, want %q", all["foo"], "rolled-back")
	}
}

func TestCloseStopsStreamUpdatesAndReturnsPromptly(t *testing.T) {
	srv := newTestServer("")
	defer srv.Close()
	srv.setSnapshot(Snapshot{Version: 1, Revision: 1, Values: map[string]any{"foo": "bar"}})

	c := New(Options{Endpoint: srv.URL, APIKey: "test-key", PollInterval: noPoll})
	if err := c.Ready(testContext(t)); err != nil {
		t.Fatalf("Ready() error = %v", err)
	}

	ch := srv.waitForStreamConn(t)

	closeDone := make(chan struct{})
	go func() {
		c.Close()
		close(closeDone)
	}()
	select {
	case <-closeDone:
	case <-time.After(2 * time.Second):
		t.Fatal("Close() did not return promptly")
	}

	// Best-effort: the connection may already be torn down by the time this
	// runs, in which case the send below is simply dropped — either way, it
	// must not change anything.
	select {
	case ch <- `data: {"version":2,"revision":99,"schemaHash":"h","values":{"foo":"should-not-apply"}}` + "\n\n":
	case <-time.After(200 * time.Millisecond):
	}

	time.Sleep(200 * time.Millisecond)
	if all := c.GetAll(); all["foo"] != "bar" {
		t.Fatalf("GetAll()[\"foo\"] = %v after Close(), want unchanged %q (a post-close frame applied)", all["foo"], "bar")
	}

	c.Close() // idempotent — must not hang or panic
}
