package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/Rohan-Muslekar/knobs/internal/delivery"
	"github.com/Rohan-Muslekar/knobs/internal/store"
)

// defaultHeartbeatInterval is how often handleStream sends a `: heartbeat`
// SSE comment while idle, keeping intermediaries (proxies, load balancers)
// from treating the connection as dead and closing it. Deps.StreamHeartbeat
// overrides this — tests set it much shorter so they don't have to wait 25s
// to observe a heartbeat.
const defaultHeartbeatInterval = 25 * time.Second

// handleStream serves GET /v1/stream, the SSE endpoint machine consumers
// hold open to learn about new config versions as they happen. Like
// handleSnapshot it sits behind apiKeyGuard: the environment comes from the
// presented key's scope, never a query parameter.
//
// Protocol: on connect, if the environment's current delivery revision is
// newer than the `since` query parameter (default 0, meaning "last revision
// seen"), that snapshot is sent immediately. Revision — not the user-facing
// config version — is the gating axis: a rollback can move version
// backwards, but revision only ever increases, so `since=<lastRevision>` is
// unambiguous even across a rollback. The connection then stays open,
// subscribed to the delivery Hub, and every subsequently published snapshot
// for this environment is sent as its own `data: <json>\n\n` frame. A `:
// heartbeat\n\n` comment is sent whenever the connection has been idle for
// Deps.StreamHeartbeat (or defaultHeartbeatInterval). The handler returns —
// unsubscribing from the Hub — as soon as the client disconnects
// (r.Context().Done()).
func (d Deps) handleStream(w http.ResponseWriter, r *http.Request) {
	envID, ok := apiKeyEnvID(r.Context())
	if !ok {
		writeErr(w, http.StatusUnauthorized, "authentication required")
		return
	}

	// deltas is opt-in: a client must ask for delta frames explicitly by
	// passing deltas=1 (or =true). Leaving it off keeps the wire format
	// byte-for-byte identical to before deltas existed — every frame a
	// bare Snapshot, no "type" field — so existing SDKs and consumers that
	// don't know about deltas keep working unchanged.
	deltasEnabled := r.URL.Query().Get("deltas") == "1" || r.URL.Query().Get("deltas") == "true"

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	// http.Server.WriteTimeout sets the connection's write deadline once,
	// at the start of the request, and never refreshes it for a
	// long-running handler — left alone, every write on this connection
	// (including the periodic heartbeat) would start failing with an i/o
	// timeout once that deadline passes, silently killing every stream
	// after a fixed number of seconds regardless of activity. Clearing it
	// here means this response is only ever bounded by the client
	// disconnecting or the server shutting down, both already handled by
	// the select loop below.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})

	env, err := d.Repo.EnvironmentByID(r.Context(), d.Repo.Pool(), envID)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "environment not found")
		return
	} else if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not load environment")
		return
	}

	var since int64
	if s := r.URL.Query().Get("since"); s != "" {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			since = n
		}
	}

	// Subscribe before computing the initial snapshot, not after: doing it
	// the other way round would leave a gap where a write that commits
	// between "read current version" and "subscribe" notifies nobody,
	// because the subscription didn't exist yet. Subscribing first means
	// the worst case is a harmless duplicate — the initial snapshot and an
	// early Publish both carry the same version — rather than a missed
	// update.
	ch, cancel := d.Hub.Subscribe(envID)
	defer cancel()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	writeSnapshot := func(snap delivery.Snapshot) bool {
		data, err := json.Marshal(snap)
		if err != nil {
			return false
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	// lastSent/haveBaseline track, for the deltas-enabled path only, the
	// most recent Snapshot this connection has actually put on the wire —
	// the baseline ComputeDelta diffs the next one against. haveBaseline
	// starts false, which is exactly what makes the very first frame sent
	// (the initial since-gated snapshot, or — if that's skipped — the
	// first frame off ch) fall through writeFrame's "no baseline yet"
	// branch and go out as a full "snapshot" frame rather than a delta.
	var lastSent delivery.Snapshot
	var haveBaseline bool

	// writeFrame is what both the initial snapshot and every subsequent
	// ch delivery call to put a frame on the wire. With deltas off it's
	// just writeSnapshot, unchanged. With deltas on: a delta frame goes
	// out only once there's a baseline to diff against *and* snap is
	// actually newer than it (guards against a stale/duplicate replay off
	// ch regressing or repeating a frame already sent); otherwise — first
	// frame, or a same-or-older repeat — a full "snapshot" frame goes out
	// instead. Either way, on a successful write lastSent/haveBaseline
	// advance to snap, so the next frame diffs against what this
	// connection actually sent, not what merely arrived.
	writeFrame := func(snap delivery.Snapshot) bool {
		if !deltasEnabled {
			return writeSnapshot(snap)
		}

		var data []byte
		var err error
		if haveBaseline && snap.Revision > lastSent.Revision {
			data, err = json.Marshal(delivery.ComputeDelta(lastSent, snap))
		} else {
			data, err = delivery.FullFrameJSON(snap)
		}
		if err != nil {
			return false
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
			return false
		}
		flusher.Flush()

		lastSent = snap
		haveBaseline = true
		return true
	}

	// Re-read the environment now, after subscribing, rather than reusing
	// the one loaded above (before subscribing): delivery_revision must
	// come from the same moment-or-later as cv below, for the same reason
	// cv itself is read after subscribing rather than before. Reusing the
	// earlier, possibly-stale env would let a write that lands in the gap
	// between the first load and Subscribe bump delivery_revision (and fire
	// a NOTIFY this connection wasn't yet subscribed to catch) while this
	// handler still gated `since` on the old revision — silently resurrecting
	// the exact missed-update race the subscribe-first ordering exists to
	// close.
	envNow, err := d.Repo.EnvironmentByID(r.Context(), d.Repo.Pool(), envID)
	if err != nil {
		log.Printf("delivery: stream %s: could not reload environment for initial snapshot: %v", envID, err)
		envNow = env
	}

	cv, err := d.Repo.CurrentVersion(r.Context(), d.Repo.Pool(), envID)
	switch {
	case err == nil && envNow.DeliveryRevision > since:
		cs, err := d.Repo.GetOrInitSchema(r.Context(), d.Repo.Pool(), envNow.ProjectID)
		if err != nil {
			// An unexpected load error on the initial snapshot is not fatal
			// to the stream: the client still benefits from staying
			// connected and receiving the next Publish, so log-and-continue
			// rather than dropping the connection. Logged (not silent) so
			// a persistently broken schema load is at least visible in
			// server logs, even though no individual caller sees it.
			log.Printf("delivery: stream %s: could not load schema for initial snapshot: %v", envID, err)
		} else if !writeFrame(delivery.BuildSnapshot(cv, cs.Definition, envNow.DeliveryRevision)) {
			return
		}
	case err != nil && !errors.Is(err, store.ErrNotFound):
		log.Printf("delivery: stream %s: could not load current version for initial snapshot: %v", envID, err)
	}

	interval := d.StreamHeartbeat
	if interval <= 0 {
		interval = defaultHeartbeatInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-d.Shutdown:
			// The server is draining for shutdown. srv.Shutdown only waits
			// out connections that are idle; an actively-streaming
			// handler like this one would otherwise never notice and
			// would hold its connection open until the shutdown timeout
			// forcibly kills it. Returning here lets this connection go
			// idle immediately, so Shutdown can complete promptly rather
			// than always burning its full timeout.
			return
		case snap, ok := <-ch:
			if !ok {
				return
			}
			if !writeFrame(snap) {
				return
			}
		case <-ticker.C:
			if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
