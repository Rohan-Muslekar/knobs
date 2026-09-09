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
  /** Targeting rules per key, evaluated locally against an EvalContext. */
  targeting?: Record<string, Rule[]>;
}

/** Identifies how a Condition compares an attribute against its configured values. */
export type Operator = "in" | "notIn" | "eq" | "neq" | "contains" | "gt" | "gte" | "lt" | "lte";

/** Tests a single attribute from the evaluation context. */
export interface Condition {
  attribute: string;
  operator: Operator;
  values: unknown[];
}

/** One weighted outcome of a Rollout. */
export interface Variant {
  value: unknown;
  weight: number;
}

/** Describes a deterministic percentage split across Variants. Salt defaults to the rule's key when empty. */
export interface Rollout {
  salt?: string;
  variants: Variant[];
}

/**
 * A single ordered targeting rule. Conditions must ALL match for the rule to apply (an empty/absent
 * conditions array is a catch-all that always matches). A matching rule resolves to `value`, or — if
 * `rollout` is set — to the bucketed variant's value.
 */
export interface Rule {
  conditions?: Condition[];
  value?: unknown;
  rollout?: Rollout;
}

/** Request-scoped data rules are evaluated against. */
export interface EvalContext {
  /** Stable identifier (e.g. user ID) rollout bucketing is keyed on. */
  targetingKey: string;
  /** Arbitrary values conditions match against. */
  attributes?: Record<string, unknown>;
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
  /**
   * Reads a single key from the in-memory snapshot. No network call.
   * With `ctx`, routes through the targeting evaluator (falling back to the raw value when no
   * rule matches); without it, returns the raw value directly.
   */
  get<T = unknown>(key: string, ctx?: EvalContext): T | undefined;
  /** Reads the full in-memory snapshot's values. No network call. Always fallback-only (no targeting). */
  getAll(): Record<string, unknown>;
  /** Evaluates every key's targeting rules against `ctx`. No network call. */
  evaluate(ctx: EvalContext): Record<string, unknown>;
  /** Registers a listener fired when the in-memory snapshot is swapped. Returns an unsubscribe fn. */
  onChange(cb: (values: Record<string, unknown>) => void): () => void;
  /** Stops any background work (streaming/poll/reconnect). Safe to call more than once. */
  close(): void;
}
