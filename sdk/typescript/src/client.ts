import { resolve, resolveAll } from "./eval.js";
import { fetchSnapshot } from "./http.js";
import { openStream } from "./stream.js";
import type { EvalContext, KnobsClient, KnobsOptions, Snapshot } from "./types.js";

const EMPTY_SNAPSHOT: Snapshot = { version: 0, revision: 0, schemaHash: "", values: {} };

/** Default slow-poll interval (belt-and-suspenders fallback to the SSE stream). */
const DEFAULT_POLL_INTERVAL_MS = 60_000;
/** Reconnect backoff: starts here, doubles on each consecutive failure, capped below. */
const INITIAL_RECONNECT_DELAY_MS = 1_000;
const MAX_RECONNECT_DELAY_MS = 30_000;

/**
 * Creates a Knobs SDK client: fetches the initial snapshot, holds it in memory for
 * zero-network `get`/`getAll` reads, then keeps it live via an SSE stream (with
 * reconnect-with-backoff) and a slow-poll fallback. Every update path — the stream, a
 * reconnect's resend, and the poll fallback — funnels through the same revision gate: a
 * snapshot only swaps in if its `revision` is strictly greater than the current one, so a
 * stale replay (equal/lower revision) is ignored while a rollback (which carries a HIGHER
 * revision even though its `version` went down) still applies.
 */
export function createClient(opts: KnobsOptions): KnobsClient {
  let current: Snapshot = EMPTY_SNAPSHOT;
  const listeners = new Set<(values: Record<string, unknown>) => void>();

  let closed = false;
  let cancelStream: (() => void) | undefined;
  let reconnectTimer: ReturnType<typeof setTimeout> | undefined;
  let pollTimer: ReturnType<typeof setInterval> | undefined;
  let reconnectDelay = INITIAL_RECONNECT_DELAY_MS;
  // Tracks the last schemaHash we've already warned about, so a long-lived process polling
  // a drifted schema every pollIntervalMs (or resuming the same drift on reconnect) doesn't
  // spam the log — we only warn again when the hash actually changes. Seeded to `undefined`
  // (distinct from any real hash, including "") so the very first check can still warn.
  let lastWarnedSchemaHash: string | undefined;

  // Warns on schemaHash drift at most once per distinct hash — shared by the first-load
  // check and every later swap (stream/reconnect/poll), so the two call sites never
  // double-warn the same hash.
  function checkSchemaDrift(snapshot: Snapshot): void {
    if (opts.expectedSchemaHash === undefined) return;
    if (snapshot.schemaHash === opts.expectedSchemaHash) return;
    if (snapshot.schemaHash === lastWarnedSchemaHash) return;
    lastWarnedSchemaHash = snapshot.schemaHash;
    console.warn(
      `knobs: schemaHash mismatch — generated code expects "${opts.expectedSchemaHash}" but the server ` +
        `is serving "${snapshot.schemaHash}". Regenerate types with \`knobs gen\`.`,
    );
  }

  // Swaps in `snapshot` only if it's strictly newer by revision, and notifies listeners.
  // Never dedupe/order by `version` — see the module doc comment above.
  function applySnapshot(snapshot: Snapshot): void {
    if (snapshot.revision <= current.revision) return;
    current = snapshot;
    checkSchemaDrift(snapshot);
    for (const cb of listeners) cb({ ...current.values });
  }

  function connectStream(): void {
    if (closed) return;
    // since = current.revision: always the latest we've applied, whether this is the first
    // connect or a reconnect after a drop — so the server resumes exactly where we left off.
    cancelStream = openStream(
      opts,
      current.revision,
      (snapshot) => {
        reconnectDelay = INITIAL_RECONNECT_DELAY_MS; // a healthy frame resets the backoff
        applySnapshot(snapshot);
      },
      () => {
        scheduleReconnect();
      },
    );
  }

  function scheduleReconnect(): void {
    if (closed) return;
    const delay = reconnectDelay;
    reconnectDelay = Math.min(reconnectDelay * 2, MAX_RECONNECT_DELAY_MS);
    reconnectTimer = setTimeout(() => {
      reconnectTimer = undefined;
      connectStream();
    }, delay);
  }

  function startPoll(): void {
    const interval = opts.pollIntervalMs ?? DEFAULT_POLL_INTERVAL_MS;
    pollTimer = setInterval(() => {
      fetchSnapshot(opts)
        .then((snapshot) => {
          if (closed || snapshot === null) return;
          applySnapshot(snapshot);
        })
        .catch(() => {
          // The poll is a belt-and-suspenders fallback to the stream (which has its own
          // reconnect); a transient poll failure isn't worth surfacing on its own.
        });
    }, interval);
  }

  const readyPromise: Promise<void> = (async () => {
    let snapshot: Snapshot | null;
    try {
      snapshot = await fetchSnapshot(opts);
    } catch (err) {
      console.warn(`knobs: initial snapshot load failed, starting empty: ${(err as Error).message}`);
      snapshot = null;
    }

    current = snapshot ?? EMPTY_SNAPSHOT;

    // Only compare when a real snapshot loaded. A 404/empty-env or failed fetch falls back
    // to the empty sentinel (schemaHash: ""), which isn't a genuine drift signal — it just
    // means there's no schema-backed snapshot to compare against yet.
    if (snapshot !== null) {
      checkSchemaDrift(current);
    }

    // Documented choice: the initial load does NOT fire onChange. ready()/getAll() already
    // deliver the baseline synchronously once ready() resolves, so a first-load onChange
    // would be redundant for a listener registered before ready(). onChange fires only for
    // snapshots swapped in later, via the stream, a reconnect, or the poll fallback.
    if (!closed) {
      connectStream();
      startPoll();
    }
  })();

  return {
    ready(): Promise<void> {
      return readyPromise;
    },

    get<T = unknown>(key: string, ctx?: EvalContext): T | undefined {
      if (ctx === undefined) return current.values[key] as T | undefined;
      return resolve(key, current.values[key], current.targeting?.[key], ctx) as T | undefined;
    },

    getAll(): Record<string, unknown> {
      // Shallow copy: `current.values` is the SDK's live in-memory snapshot, so handing it
      // out by reference would let a consumer's mutation corrupt it. See onChange below.
      return { ...current.values };
    },

    evaluate(ctx: EvalContext): Record<string, unknown> {
      return resolveAll(current.values, current.targeting, ctx);
    },

    onChange(cb: (values: Record<string, unknown>) => void): () => void {
      listeners.add(cb);
      return () => listeners.delete(cb);
    },

    close(): void {
      if (closed) return;
      closed = true;
      cancelStream?.();
      if (reconnectTimer !== undefined) clearTimeout(reconnectTimer);
      if (pollTimer !== undefined) clearInterval(pollTimer);
    },
  };
}
