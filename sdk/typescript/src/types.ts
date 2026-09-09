/** A delivered configuration snapshot for one environment. */
export interface Snapshot {
  /** The config version this snapshot was built from (can decrease on rollback). */
  version: number;
  /** Monotonically increasing delivery revision. Never dedupe/order by `version` — use this. */
  revision: number;
  /** Hash of the schema definition this snapshot's values were validated against. */
  schemaHash: string;
  /** The resolved key/value config for the environment. */
  values: Record<string, unknown>;
}

/** Options for constructing a Knobs SDK client. */
export interface KnobsOptions {
  /** Base URL of the Knobs delivery API, e.g. "https://knobs.example.com". */
  endpoint: string;
  /** API key sent as `Authorization: Bearer <apiKey>`. The environment is implied by this key. */
  apiKey: string;
  /** Logical environment name (informational; the server resolves env from the API key). */
  environment: string;
  /** Slow-poll interval in ms, used as a belt-and-suspenders fallback to streaming. Default 60_000. */
  pollIntervalMs?: number;
  /** The schemaHash the consuming code was generated against. Mismatch on first load logs a warning. */
  expectedSchemaHash?: string;
}

/** A live, in-memory view of a Knobs environment's config. */
export interface KnobsClient {
  /** Resolves once the first snapshot load attempt has settled (success or failure). */
  ready(): Promise<void>;
  /** Reads a single key from the in-memory snapshot. No network call. */
  get<T = unknown>(key: string): T | undefined;
  /** Reads the full in-memory snapshot's values. No network call. */
  getAll(): Record<string, unknown>;
  /** Registers a listener fired when the in-memory snapshot is swapped. Returns an unsubscribe fn. */
  onChange(cb: (values: Record<string, unknown>) => void): () => void;
  /** Stops any background work (streaming/poll/reconnect). Safe to call more than once. */
  close(): void;
}
