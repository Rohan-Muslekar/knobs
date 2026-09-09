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
// Protocol: on connect, if the environment's current version is newer than
// the `since` query parameter (default 0), that snapshot is sent
// immediately. The connection then stays open, subscribed to the delivery
// Hub, and every subsequently published snapshot for this environment is
// sent as its own `data: <json>\n\n` frame. A `: heartbeat\n\n` comment is
// sent whenever the connection has been idle for Deps.StreamHeartbeat (or
// defaultHeartbeatInterval). The handler returns — unsubscribing from the
// Hub — as soon as the client disconnects (r.Context().Done()).
func (d Deps) handleStream(w http.ResponseWriter, r *http.Request) {
	envID, ok := apiKeyEnvID(r.Context())
	if !ok {
		writeErr(w, http.StatusUnauthorized, "authentication required")
		return
	}

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

	since := 0
	if s := r.URL.Query().Get("since"); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
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

	cv, err := d.Repo.CurrentVersion(r.Context(), d.Repo.Pool(), envID)
	switch {
	case err == nil && cv.Version > since:
		cs, err := d.Repo.GetOrInitSchema(r.Context(), d.Repo.Pool(), env.ProjectID)
		if err != nil {
			// An unexpected load error on the initial snapshot is not fatal
			// to the stream: the client still benefits from staying
			// connected and receiving the next Publish, so log-and-continue
			// rather than dropping the connection. Logged (not silent) so
			// a persistently broken schema load is at least visible in
			// server logs, even though no individual caller sees it.
			log.Printf("delivery: stream %s: could not load schema for initial snapshot: %v", envID, err)
		} else if !writeSnapshot(delivery.BuildSnapshot(cv, cs.Definition)) {
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
			if !writeSnapshot(snap) {
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
