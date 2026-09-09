package knobs

import (
	"context"
	"testing"
	"time"
)

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
