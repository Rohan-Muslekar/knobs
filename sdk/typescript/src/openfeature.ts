import {
  ErrorCode,
  StandardResolutionReasons,
  type JsonValue,
  type Provider,
  type ResolutionDetails,
} from "@openfeature/server-sdk";
import type { KnobsClient } from "./types.js";

type ExpectedType = "boolean" | "string" | "number" | "object";

/** Reads `flagKey` off the client and shapes it into an OpenFeature ResolutionDetails. */
function resolve<T>(client: KnobsClient, flagKey: string, defaultValue: T, expectedType: ExpectedType): ResolutionDetails<T> {
  const value = client.get<unknown>(flagKey);

  if (value === undefined) {
    return { value: defaultValue, reason: StandardResolutionReasons.DEFAULT };
  }

  const matchesType = expectedType === "object" ? typeof value === "object" && value !== null : typeof value === expectedType;

  if (!matchesType) {
    return {
      value: defaultValue,
      reason: StandardResolutionReasons.ERROR,
      errorCode: ErrorCode.TYPE_MISMATCH,
      errorMessage: `knobs: flag "${flagKey}" is a ${typeof value}, expected ${expectedType}`,
    };
  }

  return { value: value as T, reason: StandardResolutionReasons.TARGETING_MATCH };
}

/**
 * An OpenFeature `Provider` backed by a `KnobsClient`'s in-memory snapshot. Resolution reads
 * are synchronous/zero-network (the client already holds the live snapshot in memory); a
 * missing key or a runtime-type mismatch against the requested flag type both fall back to
 * the caller-supplied default, the mismatch case flagged via `reason: "ERROR"` /
 * `errorCode: TYPE_MISMATCH` per the OpenFeature spec.
 */
export class KnobsProvider implements Provider {
  readonly metadata = { name: "knobs" } as const;

  constructor(private readonly client: KnobsClient) {}

  async initialize(): Promise<void> {
    await this.client.ready();
  }

  async onClose(): Promise<void> {
    this.client.close();
  }

  async resolveBooleanEvaluation(flagKey: string, defaultValue: boolean): Promise<ResolutionDetails<boolean>> {
    return resolve(this.client, flagKey, defaultValue, "boolean");
  }

  async resolveStringEvaluation(flagKey: string, defaultValue: string): Promise<ResolutionDetails<string>> {
    return resolve(this.client, flagKey, defaultValue, "string");
  }

  async resolveNumberEvaluation(flagKey: string, defaultValue: number): Promise<ResolutionDetails<number>> {
    return resolve(this.client, flagKey, defaultValue, "number");
  }

  async resolveObjectEvaluation<T extends JsonValue>(flagKey: string, defaultValue: T): Promise<ResolutionDetails<T>> {
    return resolve(this.client, flagKey, defaultValue, "object");
  }
}
