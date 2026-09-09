package knobs

import (
	"context"
	"strings"
	"testing"
	"time"
)

// runStreamForTest starts stream() in a goroutine against srv, with deltas
// requested, and returns a channel of every snapshot onSnapshot was called
// with, a channel of every delta onDelta was called with, plus a channel
// that receives stream()'s return value exactly once, when it returns.
func runStreamForTest(ctx context.Context, srv *testServer) (got chan Snapshot, gotDelta chan Delta, done chan error) {
	return runStreamForTestWithDeltas(ctx, srv, true)
}

func runStreamForTestWithDeltas(ctx context.Context, srv *testServer, useDeltas bool) (got chan Snapshot, gotDelta chan Delta, done chan error) {
	got = make(chan Snapshot, 16)
	gotDelta = make(chan Delta, 16)
	done = make(chan error, 1)
	opts := Options{Endpoint: srv.URL, APIKey: "test-key"}.withDefaults()
	go func() {
		done <- stream(ctx, opts, 0, useDeltas, func(s Snapshot) { got <- s }, func(d Delta) { gotDelta <- d })
	}()
	return got, gotDelta, done
}

func TestStreamParsesDataFrameIntoSnapshot(t *testing.T) {
	srv := newTestServer("")
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	got, _, _ := runStreamForTest(ctx, srv)
	ch := srv.waitForStreamConn(t)

	sendFrame(t, ch, `data: {"version":1,"revision":7,"schemaHash":"h1","values":{"foo":"bar"}}`+"\n\n")

	select {
	case snap := <-got:
		if snap.Revision != 7 || snap.Version != 1 || snap.SchemaHash != "h1" || snap.Values["foo"] != "bar" {
			t.Fatalf("onSnapshot got %+v, want revision=7 version=1 schemaHash=h1 values.foo=bar", snap)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for onSnapshot")
	}
}

func TestStreamIgnoresHeartbeatComments(t *testing.T) {
	srv := newTestServer("")
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	got, _, _ := runStreamForTest(ctx, srv)
	ch := srv.waitForStreamConn(t)

	// A heartbeat comment on its own must not produce a snapshot.
	sendFrame(t, ch, ": heartbeat\n\n")

	select {
	case snap := <-got:
		t.Fatalf("onSnapshot called for a heartbeat-only frame: %+v", snap)
	case <-time.After(300 * time.Millisecond):
	}

	// The connection must still be healthy afterwards — a real data frame
	// right after the heartbeat is parsed normally.
	sendFrame(t, ch, `data: {"revision":2,"values":{}}`+"\n\n")
	select {
	case snap := <-got:
		if snap.Revision != 2 {
			t.Fatalf("onSnapshot got %+v, want revision=2", snap)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for onSnapshot after the heartbeat")
	}
}

func TestStreamReassemblesFrameSplitAcrossWrites(t *testing.T) {
	srv := newTestServer("")
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	got, _, _ := runStreamForTest(ctx, srv)
	ch := srv.waitForStreamConn(t)

	// Split mid-line, with neither write containing the terminating "\n\n" —
	// the reader must buffer across both writes and only parse once the
	// frame is actually complete.
	sendFrame(t, ch, `data: {"version":3,"revi`)
	sendFrame(t, ch, `sion":9,"schemaHash":"h2","values":{"x":1}}`+"\n\n")

	select {
	case snap := <-got:
		if snap.Revision != 9 || snap.Version != 3 {
			t.Fatalf("onSnapshot got %+v, want revision=9 version=3 (frame reassembly failed)", snap)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for onSnapshot on the reassembled frame")
	}
}

func TestStreamSkipsMalformedFrameWithoutDying(t *testing.T) {
	srv := newTestServer("")
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	got, _, done := runStreamForTest(ctx, srv)
	ch := srv.waitForStreamConn(t)

	sendFrame(t, ch, "data: {not valid json\n\n")

	select {
	case snap := <-got:
		t.Fatalf("onSnapshot called for a malformed frame: %+v", snap)
	case <-time.After(300 * time.Millisecond):
	}

	// stream() must not have returned/died over the bad frame.
	select {
	case err := <-done:
		t.Fatalf("stream() returned (err=%v) after a malformed frame, want it to keep reading", err)
	default:
	}

	sendFrame(t, ch, `data: {"revision":4,"values":{}}`+"\n\n")
	select {
	case snap := <-got:
		if snap.Revision != 4 {
			t.Fatalf("onSnapshot got %+v, want revision=4", snap)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for onSnapshot after the malformed frame")
	}
}

func TestStreamAddsDeltasParamWhenRequested(t *testing.T) {
	srv := newTestServer("")
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	runStreamForTestWithDeltas(ctx, srv, true)
	srv.waitForStreamConn(t)

	if q := srv.streamQuery(); !strings.Contains(q, "deltas=1") {
		t.Errorf("stream query = %q, want it to contain deltas=1", q)
	}
}

func TestStreamOmitsDeltasParamWhenNotRequested(t *testing.T) {
	srv := newTestServer("")
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	runStreamForTestWithDeltas(ctx, srv, false)
	srv.waitForStreamConn(t)

	if q := srv.streamQuery(); strings.Contains(q, "deltas") {
		t.Errorf("stream query = %q, want no deltas param", q)
	}
}

func TestStreamParsesTypeDeltaFrameIntoDelta(t *testing.T) {
	srv := newTestServer("")
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, gotDelta, _ := runStreamForTest(ctx, srv)
	ch := srv.waitForStreamConn(t)

	sendFrame(t, ch, `data: {"type":"delta","version":2,"revision":8,"from":7,"schemaHash":"h1","values":{"set":{"foo":"bar"}}}`+"\n\n")

	select {
	case d := <-gotDelta:
		if d.Type != "delta" || d.Revision != 8 || d.From != 7 || d.Values.Set["foo"] != "bar" {
			t.Fatalf("onDelta got %+v, want revision=8 from=7 values.set.foo=bar", d)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for onDelta")
	}
}

func TestStreamParsesTypeSnapshotFrameIntoSnapshot(t *testing.T) {
	srv := newTestServer("")
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	got, _, _ := runStreamForTest(ctx, srv)
	ch := srv.waitForStreamConn(t)

	sendFrame(t, ch, `data: {"type":"snapshot","version":1,"revision":3,"schemaHash":"h1","values":{"foo":"bar"}}`+"\n\n")

	select {
	case snap := <-got:
		if snap.Revision != 3 || snap.Values["foo"] != "bar" {
			t.Fatalf("onSnapshot got %+v, want revision=3 values.foo=bar", snap)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for onSnapshot on a type:snapshot frame")
	}
}

func TestStreamReturnsPromptlyOnContextCancel(t *testing.T) {
	srv := newTestServer("")
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	_, _, done := runStreamForTest(ctx, srv)
	srv.waitForStreamConn(t) // make sure the connection is actually open first

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("stream() returned err=%v after ctx cancel, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stream() did not return promptly after ctx was cancelled")
	}
}
