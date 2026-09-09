// Package knobs is a minimal, dependency-free Go client for the Knobs delivery
// API. Knobs has no official Go SDK in v1 (TS/JS only), but the delivery API is
// plain HTTP + a Bearer read key, so a consumer can hit it directly like this.
//
// It mirrors the thick-SDK model: Snapshot fetches the current config once;
// Stream holds an SSE connection open and pushes a fresh snapshot on every
// change. Dedup on Revision (monotonic), never Version.
package knobs

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Snapshot is one environment's config at a point in time. Matches the JSON the
// server returns from GET /v1/snapshot and each SSE data frame.
type Snapshot struct {
	Version    int            `json:"version"`    // user-facing config version (can decrease on rollback)
	Revision   int64          `json:"revision"`   // monotonic delivery revision (use this for dedup/since)
	SchemaHash string         `json:"schemaHash"` // hash of the schema the values were validated against
	Values     map[string]any `json:"values"`     // JSON numbers decode as float64
}

// Client talks to a Knobs server for one environment (the env is implied by the
// API key's scope).
type Client struct {
	Endpoint string // e.g. "http://localhost:8080" (a path prefix is preserved)
	APIKey   string // an env-scoped read key: "knobs_..."
	HTTP     *http.Client
}

// New builds a Client. A trailing slash on endpoint is trimmed.
func New(endpoint, apiKey string) *Client {
	return &Client{
		Endpoint: strings.TrimRight(endpoint, "/"),
		APIKey:   apiKey,
		HTTP:     http.DefaultClient,
	}
}

func (c *Client) newRequest(ctx context.Context, path string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Endpoint+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	return req, nil
}

// Snapshot fetches the environment's current config. A 204/404 (no values set
// yet) returns (nil, nil).
func (c *Client) Snapshot(ctx context.Context) (*Snapshot, error) {
	req, err := c.newRequest(ctx, "/v1/snapshot")
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil // no values set for this environment yet
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("snapshot: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var snap Snapshot
	if err := json.NewDecoder(resp.Body).Decode(&snap); err != nil {
		return nil, fmt.Errorf("snapshot: decode: %w", err)
	}
	return &snap, nil
}

// Stream opens the SSE stream and calls onSnapshot for every pushed snapshot
// whose Revision is greater than the last one delivered (so a stale replay is
// skipped and a rollback — higher revision, lower version — still applies).
// It blocks until ctx is cancelled or the connection errors; the caller decides
// whether to reconnect (with since = last seen Revision).
func (c *Client) Stream(ctx context.Context, since int64, onSnapshot func(Snapshot)) error {
	req, err := c.newRequest(ctx, fmt.Sprintf("/v1/stream?since=%d", since))
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("stream: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	current := since
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		// SSE: blank lines separate events; ":" lines are comments (heartbeats).
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		data, ok := strings.CutPrefix(line, "data:")
		if !ok {
			continue
		}
		var snap Snapshot
		if err := json.Unmarshal([]byte(strings.TrimSpace(data)), &snap); err != nil {
			continue // skip a malformed frame rather than dropping the stream
		}
		if snap.Revision <= current {
			continue // stale/duplicate replay — ignore
		}
		current = snap.Revision
		onSnapshot(snap)
	}
	return sc.Err()
}

// Int reads an integer-valued config key (JSON numbers arrive as float64).
func (s *Snapshot) Int(key string) (int, bool) {
	if f, ok := s.Values[key].(float64); ok {
		return int(f), true
	}
	return 0, false
}

// Bool reads a boolean-valued config key.
func (s *Snapshot) Bool(key string) (bool, bool) {
	b, ok := s.Values[key].(bool)
	return b, ok
}

// String reads a string-valued config key.
func (s *Snapshot) String(key string) (string, bool) {
	v, ok := s.Values[key].(string)
	return v, ok
}
