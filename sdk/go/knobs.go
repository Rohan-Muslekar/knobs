// Package knobs is the official Go SDK for the Knobs delivery API. It mirrors
// the TypeScript SDK's contract: fetch the in-memory snapshot once via Ready,
// then read config with zero-network Get/GetAll calls. Live updates (SSE
// streaming, reconnect, and poll fallback) are wired in on top of this core
// client.
package knobs

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// defaultPollInterval is the slow-poll fallback interval used alongside
// streaming (wired in once streaming lands), when Options.PollInterval isn't set.
const defaultPollInterval = 60 * time.Second

// Reconnect backoff for the background stream loop: starts here, doubles on
// each consecutive failed connection, capped below. Reset back to the
// initial delay whenever a connection actually delivers a frame, so a flaky
// network that recovers doesn't leave the client waiting out a long backoff
// it earned from an outage that's already over.
const (
	initialReconnectDelay = 500 * time.Millisecond
	maxReconnectDelay     = 5 * time.Second
)

// Snapshot is one environment's config at a point in time — matches the JSON
// returned by GET /v1/snapshot and each SSE data frame on the delivery API.
type Snapshot struct {
	// Version is the user-facing config version; it can decrease on a rollback.
	Version int `json:"version"`
	// Revision is the monotonically increasing delivery revision. Always
	// dedupe/order by this, never by Version — a rollback carries a HIGHER
	// revision even though its Version went down, and a stale replay carries
	// an equal-or-lower one.
	Revision int64 `json:"revision"`
	// SchemaHash is the hash of the schema definition these values were validated against.
	SchemaHash string `json:"schemaHash"`
	// Values is the resolved key/value config for the environment.
	Values map[string]any `json:"values"`
	// Targeting holds the targeting rules for each key, keyed by key name.
	// GetFor and EvaluateAll evaluate these against a caller-supplied
	// EvalContext; Get/GetAll ignore them entirely.
	Targeting map[string][]Rule `json:"targeting"`
}

// emptySnapshot is what a Client holds when the server has no values for an
// environment yet (a 404 from /v1/snapshot).
var emptySnapshot = Snapshot{Values: map[string]any{}, Targeting: map[string][]Rule{}}

// Options configures a Client.
type Options struct {
	// Endpoint is the base URL of the Knobs delivery API, e.g. "https://knobs.example.com".
	// A path prefix (e.g. from a reverse-proxy mount) is preserved.
	Endpoint string
	// APIKey is sent as `Authorization: Bearer <APIKey>`. The environment is implied by this key.
	APIKey string
	// Environment is informational; the server resolves the actual environment from the API key.
	Environment string
	// PollInterval is the slow-poll interval used as a fallback alongside
	// streaming. Zero or negative defaults to 60s.
	PollInterval time.Duration
	// ExpectedSchemaHash is the schemaHash the consuming generated code was
	// built against. A mismatch against a real (non-404) snapshot logs a
	// warning — at most once per distinct hash — rather than failing.
	ExpectedSchemaHash string
	// HTTPClient is used for all requests. Defaults to http.DefaultClient.
	HTTPClient *http.Client
	// Logger receives drift warnings and other diagnostics. Defaults to slog.Default().
	Logger *slog.Logger
	// DisableDeltas opts out of incremental delta frames on the SSE stream.
	// Deltas are ON by default (the zero value, false, keeps them enabled) —
	// this is a disable flag rather than an "enable" one specifically so a
	// bare Options{} still gets the smaller-payload behavior without every
	// caller having to opt in. When enabled, the client requests
	// ?deltas=1 on the stream URL and, on a "delta" frame, applies it on top
	// of the in-memory snapshot (falling back to a full resync if the delta
	// doesn't chain onto the client's current revision) instead of always
	// waiting for/requesting a full snapshot body.
	DisableDeltas bool
}

// withDefaults returns opts with the documented zero-value defaults filled in.
func (o Options) withDefaults() Options {
	if o.HTTPClient == nil {
		o.HTTPClient = http.DefaultClient
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	if o.PollInterval <= 0 {
		o.PollInterval = defaultPollInterval
	}
	return o
}

// Client is a live, in-memory view of one Knobs environment's config. Build
// one with New, call Ready to load the first snapshot, then read with Get or
// GetAll — both are zero-network reads against the in-memory copy.
type Client struct {
	opts Options

	mu      sync.RWMutex
	current Snapshot

	listenersMu    sync.Mutex
	listeners      map[int]func(map[string]any)
	nextListenerID int

	warnMu         sync.Mutex
	warnedOnce     bool
	lastWarnedHash string

	closeMu sync.Mutex
	closed  bool
	// cancel stops the background stream/poll goroutines Ready launches once
	// live updates are wired in; Close calls it. Nil until then.
	cancel context.CancelFunc
}

// New builds a Client from opts. It does not make any network calls — call
// Ready to load the first snapshot before reading config.
func New(opts Options) *Client {
	return &Client{
		opts:      opts.withDefaults(),
		current:   emptySnapshot,
		listeners: make(map[int]func(map[string]any)),
	}
}

// Ready fetches the first snapshot and stores it in memory, then launches
// the background work that keeps it live: an SSE stream (reconnecting with
// backoff on drop) and a slow-poll fallback, both funnelled through the same
// revision-gated applySnapshot as this initial load. A 404 (no values set
// yet for the environment) is not an error — the Client just starts out
// with an empty snapshot. If Options.ExpectedSchemaHash is set and a real
// (non-404) snapshot's SchemaHash differs, it logs a warning via the
// configured Logger (at most once per distinct hash).
//
// On a non-404 fetch error (network/5xx/auth), Ready returns that error and
// does NOT start the background work — so the Client is not live and the
// caller should retry Ready. This differs from the TS SDK, which never
// rejects and always starts streaming; the Go SDK makes the first fetch a
// hard gate so a misconfigured endpoint/key surfaces at startup instead of
// silently serving an empty snapshot.
func (c *Client) Ready(ctx context.Context) error {
	snap, err := fetchSnapshot(ctx, c.opts, 0)
	if err != nil {
		return err
	}

	if snap == nil {
		// No values set for this environment yet — not a drift signal, since
		// there's no schema-backed snapshot to compare against.
		c.setSnapshot(emptySnapshot)
	} else {
		c.setSnapshot(*snap)
		c.checkSchemaDrift(*snap)
	}

	c.startBackground()
	return nil
}

// startBackground launches the background stream + poll goroutine(s) bound
// to a context Close cancels. A no-op if Close already ran (Ready racing a
// concurrent Close that got there first) or if it's already been started —
// Ready is meant to be called once, but this keeps a second call harmless
// instead of leaking a duplicate set of goroutines.
func (c *Client) startBackground() {
	c.closeMu.Lock()
	defer c.closeMu.Unlock()
	if c.closed || c.cancel != nil {
		return
	}

	bgCtx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel

	go c.runStream(bgCtx)
	go c.runPoll(bgCtx)
}

// runStream keeps an SSE connection open for the lifetime of ctx, applying
// every frame it receives through applySnapshot and reconnecting — starting
// from the latest revision the client has actually applied — after a capped
// backoff whenever the connection drops. It returns once ctx is cancelled.
func (c *Client) runStream(ctx context.Context) {
	delay := initialReconnectDelay
	useDeltas := !c.opts.DisableDeltas

	for ctx.Err() == nil {
		gotFrame := false
		err := stream(ctx, c.opts, c.currentRevision(), useDeltas, func(snap Snapshot) {
			gotFrame = true
			c.applySnapshot(snap)
		}, func(d Delta) {
			gotFrame = true
			c.applyDeltaFrame(ctx, d)
		})
		if ctx.Err() != nil {
			return
		}

		if gotFrame {
			// The connection was healthy enough to deliver at least one
			// frame — whatever knocked it over just now isn't necessarily
			// the same outage the backoff was climbing for, so start over.
			delay = initialReconnectDelay
		}
		if err != nil {
			c.opts.Logger.Warn("knobs: stream connection dropped, reconnecting", "error", err, "retryIn", delay)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}

		delay *= 2
		if delay > maxReconnectDelay {
			delay = maxReconnectDelay
		}
	}
}

// runPoll is the slow-poll fallback alongside the stream: on every tick it
// fetches a fresh snapshot and applies it through the same revision gate.
// It returns once ctx is cancelled.
func (c *Client) runPoll(ctx context.Context) {
	ticker := time.NewTicker(c.opts.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			snap, err := fetchSnapshot(ctx, c.opts, 0)
			if err != nil || snap == nil {
				// A transient poll failure isn't worth surfacing on its own —
				// the stream (with its own reconnect) is the primary path;
				// this is belt-and-suspenders.
				continue
			}
			c.applySnapshot(*snap)
		}
	}
}

// setSnapshot stores snap as the current in-memory snapshot.
func (c *Client) setSnapshot(snap Snapshot) {
	if snap.Values == nil {
		snap.Values = map[string]any{}
	}
	if snap.Targeting == nil {
		snap.Targeting = map[string][]Rule{}
	}
	c.mu.Lock()
	c.current = snap
	c.mu.Unlock()
}

// currentRevision returns the Revision of the in-memory snapshot. Used as
// the since cursor for a (re)connecting stream and logged/compared without
// ever taking the write lock just to read one field.
func (c *Client) currentRevision() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.current.Revision
}

// applySnapshot is the single funnel every live-update path — the stream,
// its reconnects, and the poll fallback — swaps snapshots through. It only
// swaps in snap if its Revision is strictly greater than the current one:
// an equal-or-lower revision is a stale replay and is ignored, while a
// rollback (a HIGHER revision whose Version went down) still applies. See
// the Snapshot.Revision doc comment — never gate on Version.
//
// On a genuine swap it re-checks schemaHash drift and fires every OnChange
// listener with its own copy of the new values, so a listener mutating what
// it was handed can't corrupt the Client's state or another listener's copy.
func (c *Client) applySnapshot(snap Snapshot) {
	if snap.Values == nil {
		snap.Values = map[string]any{}
	}
	if snap.Targeting == nil {
		snap.Targeting = map[string][]Rule{}
	}

	c.mu.Lock()
	if snap.Revision <= c.current.Revision {
		c.mu.Unlock()
		return
	}
	c.current = snap
	c.mu.Unlock()

	c.checkSchemaDrift(snap)
	c.notifyListeners(snap.Values)
}

// applyDeltaFrame handles one "delta" frame from the stream. If it chains
// cleanly onto the in-memory snapshot — d.From equals the Revision the
// client currently holds, read under the same lock applySnapshot uses —
// it computes the resulting snapshot with applyDelta and feeds it through
// applySnapshot, so the revision-gate and listener notification run
// through that single funnel exactly as a full snapshot frame would.
//
// If it doesn't chain (a dropped delta, a reordered frame, or a fresh
// reconnect that's behind), that's the defensive case the wire contract
// calls for a full resync on: refetch a full snapshot via fetchSnapshot and
// apply that instead. A resync failure (network error, or the environment
// having no values) is swallowed here the same way runPoll swallows one —
// the stream's own reconnect loop, or the next poll tick, is the retry.
func (c *Client) applyDeltaFrame(ctx context.Context, d Delta) {
	c.mu.RLock()
	current := c.current
	c.mu.RUnlock()

	if d.From == current.Revision {
		c.applySnapshot(applyDelta(current, d))
		return
	}

	snap, err := fetchSnapshot(ctx, c.opts, 0)
	if err != nil || snap == nil {
		return
	}
	c.applySnapshot(*snap)
}

// notifyListeners calls every registered OnChange callback with its own
// copy of values. Listeners are snapshotted under listenersMu and then
// invoked outside the lock, so a callback that calls unsubscribe (or
// registers a new listener) doesn't deadlock against listenersMu.
func (c *Client) notifyListeners(values map[string]any) {
	c.listenersMu.Lock()
	cbs := make([]func(map[string]any), 0, len(c.listeners))
	for _, cb := range c.listeners {
		cbs = append(cbs, cb)
	}
	c.listenersMu.Unlock()

	for _, cb := range cbs {
		valuesCopy := make(map[string]any, len(values))
		for k, v := range values {
			valuesCopy[k] = v
		}
		cb(valuesCopy)
	}
}

// OnChange registers cb to be called, with a copy of the current values,
// every time a live update (stream or poll) swaps in a newer snapshot. It
// is NOT called for the initial snapshot Ready loads — a listener
// registered before Ready already gets that baseline from Ready/GetAll
// directly. Returns an unsubscribe function; calling it more than once is
// safe.
func (c *Client) OnChange(cb func(values map[string]any)) (unsubscribe func()) {
	c.listenersMu.Lock()
	id := c.nextListenerID
	c.nextListenerID++
	c.listeners[id] = cb
	c.listenersMu.Unlock()

	return func() {
		c.listenersMu.Lock()
		delete(c.listeners, id)
		c.listenersMu.Unlock()
	}
}

// checkSchemaDrift warns when snap's SchemaHash doesn't match
// Options.ExpectedSchemaHash, at most once per distinct hash seen — so a
// long-lived process re-checking the same drift on every poll or reconnect
// doesn't spam the log.
func (c *Client) checkSchemaDrift(snap Snapshot) {
	if c.opts.ExpectedSchemaHash == "" {
		return
	}
	if snap.SchemaHash == c.opts.ExpectedSchemaHash {
		return
	}

	c.warnMu.Lock()
	defer c.warnMu.Unlock()
	if c.warnedOnce && c.lastWarnedHash == snap.SchemaHash {
		return
	}
	c.warnedOnce = true
	c.lastWarnedHash = snap.SchemaHash

	c.opts.Logger.Warn(
		"knobs: schemaHash mismatch — generated code expects a different schema than the server is serving; regenerate types with `knobs gen`",
		"expected", c.opts.ExpectedSchemaHash,
		"actual", snap.SchemaHash,
	)
}

// Get reads a single key from the in-memory snapshot. No network call.
func (c *Client) Get(key string) (any, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.current.Values[key]
	return v, ok
}

// GetAll returns a copy of the in-memory snapshot's values. No network call.
// The returned map is a fresh copy each time, so a caller mutating it can
// never corrupt the Client's internal state.
func (c *Client) GetAll() map[string]any {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make(map[string]any, len(c.current.Values))
	for k, v := range c.current.Values {
		out[k] = v
	}
	return out
}

// Close stops any background work Ready started (streaming, its reconnect
// loop, and the poll fallback) by cancelling the context they all share.
// It returns as soon as that cancellation is issued — it does not block on
// the goroutines actually exiting, though they do so promptly since the
// in-flight stream request is bound to the same context. Safe to call more
// than once, and safe to call even if Ready was never called.
func (c *Client) Close() {
	c.closeMu.Lock()
	defer c.closeMu.Unlock()
	if c.closed {
		return
	}
	c.closed = true
	if c.cancel != nil {
		c.cancel()
	}
}
