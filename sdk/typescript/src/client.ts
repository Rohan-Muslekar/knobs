import { fetchSnapshot } from "./http.js";
import type { KnobsClient, KnobsOptions, Snapshot } from "./types.js";

const EMPTY_SNAPSHOT: Snapshot = { version: 0, revision: 0, schemaHash: "", values: {} };

/**
 * Creates a Knobs SDK client: fetches the initial snapshot and holds it in memory for
 * zero-network `get`/`getAll` reads. Live streaming (SSE + reconnect + poll) is wired up
 * in Task 3; `close()` here is a placeholder for that lifecycle.
 */
export function createClient(opts: KnobsOptions): KnobsClient {
  let current: Snapshot = EMPTY_SNAPSHOT;
  const listeners = new Set<(values: Record<string, unknown>) => void>();

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
    if (snapshot !== null && opts.expectedSchemaHash !== undefined && current.schemaHash !== opts.expectedSchemaHash) {
      console.warn(
        `knobs: schemaHash mismatch — generated code expects "${opts.expectedSchemaHash}" but the server ` +
          `is serving "${current.schemaHash}". Regenerate types with \`knobs gen\`.`,
      );
    }

    // Documented choice: the initial load does NOT fire onChange. ready()/getAll() already
    // deliver the baseline synchronously once ready() resolves, so a first-load onChange
    // would be redundant for a listener registered before ready(). onChange is reserved for
    // snapshots swapped in later (Task 3's streaming/poll updates).
  })();

  return {
    ready(): Promise<void> {
      return readyPromise;
    },

    get<T = unknown>(key: string): T | undefined {
      return current.values[key] as T | undefined;
    },

    getAll(): Record<string, unknown> {
      return current.values;
    },

    onChange(cb: (values: Record<string, unknown>) => void): () => void {
      listeners.add(cb);
      return () => listeners.delete(cb);
    },

    close(): void {
      // No-op placeholder: Task 3 makes this cancel the stream/poll/reconnect timers.
    },
  };
}
