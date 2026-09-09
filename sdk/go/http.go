package knobs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// fetchSnapshot fetches the current snapshot from GET {endpoint}/v1/snapshot.
//
// The endpoint's URL is built by trimming a trailing slash off opts.Endpoint
// and appending, rather than resolving it as a base+path via the stdlib URL
// joining rules — the latter would reset the path and silently drop any
// prefix (e.g. a reverse-proxy mount like "https://gw.example.com/knobs"
// would become "https://gw.example.com/v1/snapshot").
//
// since is accepted for forward-compat with reconnect-with-since (wired in
// Task 2's streaming); the core client passes 0 and it's omitted from the URL.
//
//   - 404 (no values set for the environment yet) -> (nil, nil).
//   - Any other non-2xx -> an error carrying the server's {"error": "..."} message.
func fetchSnapshot(ctx context.Context, opts Options, since int64) (*Snapshot, error) {
	base := strings.TrimRight(opts.Endpoint, "/")
	url := base + "/v1/snapshot"
	if since > 0 {
		url += fmt.Sprintf("?since=%d", since)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+opts.APIKey)

	client := opts.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("knobs: failed to fetch snapshot (%d): %s", resp.StatusCode, readErrorMessage(resp))
	}

	var snap Snapshot
	if err := json.NewDecoder(resp.Body).Decode(&snap); err != nil {
		return nil, fmt.Errorf("knobs: decode snapshot: %w", err)
	}
	return &snap, nil
}

// readErrorMessage extracts the server's {"error": "..."} message from a
// non-2xx response body, falling back to the raw body or the HTTP status
// text if the body isn't the expected shape.
func readErrorMessage(resp *http.Response) string {
	body, err := io.ReadAll(resp.Body)
	if err != nil || len(body) == 0 {
		return resp.Status
	}

	var parsed struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err == nil && parsed.Error != "" {
		return parsed.Error
	}

	if msg := strings.TrimSpace(string(body)); msg != "" {
		return msg
	}
	return resp.Status
}
