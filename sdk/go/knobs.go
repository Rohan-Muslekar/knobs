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
}

// emptySnapshot is what a Client holds when the server has no values for an
// environment yet (a 404 from /v1/snapshot).
var emptySnapshot = Snapshot{Values: map[string]any{}}

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

// Ready fetches the first snapshot and stores it in memory. A 404 (no values
// set yet for the environment) is not an error — the Client just starts out
// with an empty snapshot. If Options.ExpectedSchemaHash is set and a real
// (non-404) snapshot's SchemaHash differs, it logs a warning via the
// configured Logger (at most once per distinct hash).
//
// TODO(streaming): once live updates land, Ready will also launch the
// background stream + poll goroutines here, bound to a context that Close
// cancels. For now it only performs the one-shot initial load.
func (c *Client) Ready(ctx context.Context) error {
	snap, err := fetchSnapshot(ctx, c.opts, 0)
	if err != nil {
		return err
	}

	if snap == nil {
		// No values set for this environment yet — not a drift signal, since
		// there's no schema-backed snapshot to compare against.
		c.setSnapshot(emptySnapshot)
		return nil
	}

	c.setSnapshot(*snap)
	c.checkSchemaDrift(*snap)
	return nil
}

// setSnapshot stores snap as the current in-memory snapshot.
func (c *Client) setSnapshot(snap Snapshot) {
	if snap.Values == nil {
		snap.Values = map[string]any{}
	}
	c.mu.Lock()
	c.current = snap
	c.mu.Unlock()
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

// Close stops any background work Ready started (streaming/poll/reconnect,
// once wired in). Safe to call more than once, and safe to call even if
// Ready was never called.
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
