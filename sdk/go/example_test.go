package knobs

import (
	"testing"
	"time"
)

// TestClientLifecycleE2E is the capstone lifecycle test for the Go SDK: it
// drives the whole documented usage flow — New, Ready (which loads the
// first snapshot AND starts the background stream), an initial read via
// GetAll, a live update delivered over the SSE stream that both updates
// GetAll and fires an OnChange listener, and finally Close — against the
// same httptest fake server the rest of the package's tests use. It's
// bounded throughout (testContext's 5s timeout, plus 2s waits on every
// blocking select) so a wiring regression fails fast instead of hanging.
func TestClientLifecycleE2E(t *testing.T) {
	srv := newTestServer("")
	defer srv.Close()
	srv.setSnapshot(Snapshot{
		Version:    1,
		Revision:   1,
		SchemaHash: "h1",
		Values:     map[string]any{"maxRetries": float64(3), "featureX": false},
	})

	// New makes no network calls.
	c := New(Options{
		Endpoint:     srv.URL,
		APIKey:       "test-key",
		Environment:  "prod",
		PollInterval: noPoll, // isolate the assertions to the stream path
	})

	ctx := testContext(t)
	if err := c.Ready(ctx); err != nil {
		t.Fatalf("Ready() error = %v", err)
	}
	defer c.Close()

	// Initial read, right after Ready: zero-network, served straight from
	// the snapshot Ready just loaded.
	all := c.GetAll()
	if all["maxRetries"] != float64(3) || all["featureX"] != false {
		t.Fatalf("GetAll() after Ready = %v, want the served initial snapshot", all)
	}
	if v, ok := c.Get("maxRetries"); !ok || v != float64(3) {
		t.Fatalf("Get(%q) = (%v, %v), want (3, true)", "maxRetries", v, ok)
	}

	// Subscribe before the live update so we can synchronize on it landing,
	// rather than polling GetAll in a loop.
	changed := make(chan map[string]any, 1)
	unsubscribe := c.OnChange(func(values map[string]any) { changed <- values })
	defer unsubscribe()

	// Drive a streamed snapshot with a higher revision through the fake
	// server's controllable SSE connection — this is Ready's background
	// stream goroutine actually reading a live frame, not a direct call
	// into applySnapshot.
	ch := srv.waitForStreamConn(t)
	sendFrame(t, ch, `data: {"version":2,"revision":2,"schemaHash":"h1","values":{"maxRetries":5,"featureX":true}}`+"\n\n")

	select {
	case values := <-changed:
		if values["maxRetries"] != float64(5) || values["featureX"] != true {
			t.Fatalf("OnChange values = %v, want the streamed update", values)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for OnChange after the streamed update")
	}

	// The in-memory snapshot GetAll/Get read from must reflect the same
	// update the OnChange listener just saw.
	all = c.GetAll()
	if all["maxRetries"] != float64(5) || all["featureX"] != true {
		t.Fatalf("GetAll() after streamed update = %v, want maxRetries=5 featureX=true", all)
	}

	// Close must stop the background work promptly and be safe to call
	// again (e.g. from a deferred cleanup after an explicit early Close).
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
	c.Close() // idempotent
}
