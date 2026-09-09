package delivery

import (
	"context"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// NotifyChannel is the single Postgres NOTIFY channel the value-save and
// rollback write paths publish on, and the one Listener subscribes to. It
// is exported so both sides can reference the same constant rather than
// duplicating the string.
const NotifyChannel = "knobs_env"

// reconnectBackoff is how long Listener waits before retrying LISTEN after
// its connection drops (network blip, Postgres restart, pool exhaustion,
// ...). It is small and constant rather than exponential: a Listener that's
// down for any real length of time is a production incident, not something
// worth backing off softly from, and a moment's constant delay is enough to
// avoid hot-looping on Acquire.
const reconnectBackoff = time.Second

// closeTimeout bounds how long listenOnce waits for its dedicated
// connection to close (best-effort protocol shutdown) when it's done with
// it, e.g. because ctx was cancelled. ctx itself may already be Done at
// that point, so Close uses its own short-lived context rather than the
// (possibly already-expired) one Run was given.
const closeTimeout = 5 * time.Second

// SnapshotLoader loads the current Snapshot for envID, e.g. by reading the
// environment's current config_version and its project's schema. The bool
// is false when there is nothing to publish (environment or version
// missing, load error) — the caller drops the notification rather than
// publishing a zero-value Snapshot.
type SnapshotLoader func(ctx context.Context, envID uuid.UUID) (Snapshot, bool)

// Listener runs a dedicated Postgres connection LISTENing on NotifyChannel
// and republishes every notification through a Hub, so any number of SSE
// connections on this server instance — no matter which instance's write
// path produced the change — learn about a new config version.
type Listener struct {
	databaseURL string
	hub         *Hub
	loader      SnapshotLoader
}

// NewListener builds a Listener. databaseURL is used to open a single raw,
// non-pooled connection (pgx.Connect) per LISTEN session — deliberately
// NOT an *pgxpool.Pool connection. A connection that has issued `LISTEN
// knobs_env` carries that as permanent session state for its lifetime;
// pgx/pgxpool has no way to detect or reset that state on Release, so a
// LISTENing connection acquired from the shared query pool and later
// released back into it would silently keep listening while sitting in the
// pool, available to be handed out for an unrelated query. Owning the
// connection directly, and closing it (never releasing it) when the
// listen session ends, keeps the LISTEN session fully isolated from the
// pool the rest of the app queries through.
func NewListener(databaseURL string, hub *Hub, loader SnapshotLoader) *Listener {
	return &Listener{databaseURL: databaseURL, hub: hub, loader: loader}
}

// Run listens for notifications until ctx is cancelled. Any connection
// error (dropped connection, Acquire failure, ...) is logged and retried
// after reconnectBackoff; ctx cancellation is the only way Run returns.
func (l *Listener) Run(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		err := l.listenOnce(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			log.Printf("delivery: listener error, reconnecting: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(reconnectBackoff):
		}
	}
}

// listenOnce opens one dedicated, non-pooled connection, issues LISTEN, and
// pumps notifications into the Hub until ctx is cancelled or the connection
// fails. The connection is always closed (never released to a pool) when
// this returns, so a LISTEN session can never leak into shared query
// traffic.
func (l *Listener) listenOnce(ctx context.Context) error {
	conn, err := pgx.Connect(ctx, l.databaseURL)
	if err != nil {
		return err
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), closeTimeout)
		defer cancel()
		_ = conn.Close(closeCtx)
	}()

	if _, err := conn.Exec(ctx, "LISTEN "+NotifyChannel); err != nil {
		return err
	}

	for {
		notif, err := conn.WaitForNotification(ctx)
		if err != nil {
			return err
		}
		envID, ok := parsePayload(notif.Payload)
		if !ok {
			log.Printf("delivery: listener: unparseable notification payload %q", notif.Payload)
			continue
		}
		snap, ok := l.loader(ctx, envID)
		if !ok {
			continue
		}
		l.hub.Publish(envID, snap)
	}
}

// parsePayload parses a NOTIFY payload of the form "<envID>:<version>" and
// returns the environment id. The version half is intentionally not
// returned: the loader always fetches the current version fresh rather than
// trusting the payload's, so a burst of rapid writes can never leave a
// subscriber stuck on a stale version parsed from an out-of-order or
// coalesced notification.
func parsePayload(payload string) (uuid.UUID, bool) {
	idPart, versionPart, found := strings.Cut(payload, ":")
	if !found {
		return uuid.Nil, false
	}
	if _, err := strconv.Atoi(versionPart); err != nil {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(idPart)
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}
