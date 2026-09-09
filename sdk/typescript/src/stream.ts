import type { KnobsOptions, Snapshot } from "./types.js";

/**
 * Opens the SSE live-update stream at `GET {endpoint}/v1/stream?since={since}`.
 *
 * Reads `res.body` as a byte stream, buffers it, and splits on the SSE frame delimiter
 * (`\n\n`) — a frame may arrive split across multiple chunk boundaries, so bytes are
 * accumulated across `reader.read()` calls rather than parsed chunk-by-chunk. Within a
 * frame, `:`-prefixed lines are comments (the server's periodic `: heartbeat` keepalive)
 * and are ignored; `data:`-prefixed lines are joined and parsed as JSON into a `Snapshot`,
 * then handed to `onSnapshot`. Any fetch failure, non-2xx response, malformed payload, or
 * the server ending the connection (stream closes without being cancelled) is reported via
 * `onError` — the caller (client.ts) is responsible for reconnecting.
 *
 * Returns a cancel function that aborts the underlying fetch/stream. After cancel, no
 * further `onSnapshot`/`onError` calls are made — this keeps `close()` leak-free and keeps
 * tests deterministic (a cancelled stream simply stops, it doesn't surface a spurious error).
 */
export function openStream(
  opts: KnobsOptions,
  since: number,
  onSnapshot: (snapshot: Snapshot) => void,
  onError: (err: unknown) => void,
): () => void {
  // Same trim-and-concat pattern as http.ts: appending to the endpoint rather than
  // `new URL(path, base)`, which would reset the path and drop a reverse-proxy prefix.
  const base = opts.endpoint.replace(/\/+$/, "");
  const url = `${base}/v1/stream?since=${encodeURIComponent(String(since))}`;

  const controller = new AbortController();
  let cancelled = false;
  let reader: ReadableStreamDefaultReader<Uint8Array> | undefined;

  controller.signal.addEventListener("abort", () => {
    cancelled = true;
    // Release the reader's lock and signal the source to stop producing. Ignored if the
    // reader doesn't exist yet (cancelled before the fetch resolved) or is already released.
    reader?.cancel().catch(() => {});
  });

  void (async () => {
    try {
      const res = await fetch(url, {
        headers: {
          authorization: `Bearer ${opts.apiKey}`,
          accept: "text/event-stream",
        },
        signal: controller.signal,
      });
      if (cancelled) return;

      if (!res.ok || !res.body) {
        throw new Error(`knobs: stream request failed (${res.status})`);
      }

      reader = res.body.getReader();
      const decoder = new TextDecoder();
      let buffer = "";

      while (!cancelled) {
        const { done, value } = await reader.read();
        if (done) break;

        buffer += decoder.decode(value, { stream: true });

        let sep: number;
        while ((sep = buffer.indexOf("\n\n")) !== -1) {
          const frame = buffer.slice(0, sep);
          buffer = buffer.slice(sep + 2);
          handleFrame(frame);
        }
      }

      // The read loop above ends either because the stream was cancelled (handled by the
      // abort listener already) or because the server closed the connection on its own —
      // the latter is reported so client.ts can reconnect.
      if (!cancelled) {
        onError(new Error("knobs: stream closed by server"));
      }
    } catch (err) {
      if (cancelled) return;
      onError(err);
    }
  })();

  // Deliberately lets a JSON parse failure propagate to the outer try/catch rather than
  // catching it here: that keeps `onError` called at most once per connection (matching the
  // fetch-failure and stream-closed paths below) and stops reading this connection, so
  // client.ts's "one onError -> one scheduled reconnect" contract holds. Catching and
  // continuing here instead would let a single connection fire onError repeatedly — once
  // per malformed frame — stacking duplicate reconnects/connections in the caller.
  function handleFrame(frame: string): void {
    const dataLines: string[] = [];
    for (const line of frame.split("\n")) {
      if (line.startsWith(":")) continue; // heartbeat/comment — ignore
      if (line.startsWith("data:")) {
        dataLines.push(line.slice("data:".length).replace(/^ /, ""));
      }
    }
    if (dataLines.length === 0) return;

    const snapshot = JSON.parse(dataLines.join("\n")) as Snapshot;
    onSnapshot(snapshot);
  }

  return function cancel(): void {
    controller.abort();
  };
}
