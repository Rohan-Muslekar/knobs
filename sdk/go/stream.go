package knobs

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// maxSSELineLength caps a single SSE line the scanner will accept. The
// server's snapshots are small config blobs, not arbitrary uploads, but the
// default bufio.Scanner token limit (64KB) is tight enough that a bigger
// config could legitimately trip it, so it's raised well past that.
const maxSSELineLength = 1024 * 1024 // 1MB

// frameEnvelope is unmarshalled first from every SSE data frame just to peek
// its "type" discriminator, before deciding whether to parse the frame as a
// Delta or a Snapshot.
type frameEnvelope struct {
	Type string `json:"type"`
}

// stream opens the SSE live-update connection at
// GET {endpoint}/v1/stream?since={since} (with "&deltas=1" appended when
// useDeltas is true) and hands every well-formed frame to onSnapshot or
// onDelta until the connection ends.
//
// Framing follows the SSE text format the delivery API speaks: a blank line
// ends an event, a line starting with ":" is a comment (the server's
// periodic heartbeat keepalive) and is ignored, and "data:" lines carry the
// JSON frame — possibly split across more than one "data:" line, in which
// case they're joined with "\n" before parsing, matching the TS SDK. Each
// frame's "type" field is peeked first: "delta" is parsed as a Delta and
// handed to onDelta; "snapshot", or no "type" at all (an older, non-delta
// server), is parsed as a Snapshot and handed to onSnapshot. A frame whose
// data doesn't unmarshal into the shape its type calls for is skipped
// rather than treated as fatal, since one bad frame shouldn't take down a
// long-lived connection. (This is a deliberate divergence from the TS SDK,
// which aborts and reconnects on a malformed frame; here we keep reading,
// on the view that a single corrupt event is more likely transient than a
// poisoned stream.)
//
// The request is built with http.NewRequestWithContext, so cancelling ctx
// aborts the underlying connection and unblocks the read loop — stream then
// returns nil rather than surfacing ctx.Err() as a real failure, since a
// cancelled stream isn't an error condition for the caller (Close, or a
// context that the caller owns and controls the lifetime of).
func stream(ctx context.Context, opts Options, since int64, useDeltas bool, onSnapshot func(Snapshot), onDelta func(Delta)) error {
	base := strings.TrimRight(opts.Endpoint, "/")
	url := fmt.Sprintf("%s/v1/stream?since=%d", base, since)
	if useDeltas {
		url += "&deltas=1"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+opts.APIKey)
	req.Header.Set("Accept", "text/event-stream")

	client := opts.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("knobs: stream request failed (%d): %s", resp.StatusCode, readErrorMessage(resp))
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), maxSSELineLength)

	var dataLines []string
	for scanner.Scan() {
		line := scanner.Text()

		switch {
		case line == "":
			// Blank line: end of this event. Parse whatever data: lines we've
			// accumulated (if any — a heartbeat-only frame has none) and reset
			// for the next one.
			if len(dataLines) > 0 {
				raw := []byte(strings.Join(dataLines, "\n"))

				var env frameEnvelope
				if err := json.Unmarshal(raw, &env); err == nil {
					if env.Type == "delta" {
						var d Delta
						if err := json.Unmarshal(raw, &d); err == nil {
							onDelta(d)
						}
					} else {
						// "snapshot", or no "type" at all — a bare frame
						// from a server that doesn't speak delta mode.
						var snap Snapshot
						if err := json.Unmarshal(raw, &snap); err == nil {
							onSnapshot(snap)
						}
					}
				}
				dataLines = dataLines[:0]
			}
		case strings.HasPrefix(line, ":"):
			// Heartbeat/comment line — ignored.
		case strings.HasPrefix(line, "data:"):
			d := strings.TrimPrefix(line, "data:")
			d = strings.TrimPrefix(d, " ")
			dataLines = append(dataLines, d)
		default:
			// Any other SSE field (event:, id:, retry:) isn't part of this
			// API's contract — ignore rather than error, for forward-compat.
		}
	}

	if err := scanner.Err(); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}

	// scanner.Scan() returned false with no error: the server closed the
	// connection on its own. If that's because ctx was cancelled out from
	// under us (the read got aborted), treat it the same as any other
	// ctx-cancel exit; otherwise it's a real disconnect the caller should
	// reconnect on.
	if ctx.Err() != nil {
		return nil
	}
	return fmt.Errorf("knobs: stream closed by server")
}
