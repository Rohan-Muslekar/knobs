import { vi } from "vitest";
import type { Snapshot } from "../types.js";

/** A scripted response for the fake `/v1/snapshot` endpoint. */
export type ScriptedSnapshotResponse =
  | { status: 200; body: Snapshot }
  | { status: 404; body?: { error: string } }
  | { status: number; body?: { error: string } };

export interface FakeServerOptions {
  endpoint: string;
  apiKey: string;
  snapshot: ScriptedSnapshotResponse;
}

export interface CapturedRequest {
  url: string;
  headers: Headers;
}

export interface FakeServerHandle {
  /** Every request the stub observed, in order. */
  requests: CapturedRequest[];
  /** Restores the real `globalThis.fetch`. Call from `afterEach`. */
  restore(): void;
}

const realFetch = globalThis.fetch;

/**
 * Replaces `globalThis.fetch` with a stub that serves a scripted response for
 * `${endpoint}/v1/snapshot` and enforces the `Authorization: Bearer <apiKey>` header,
 * mirroring the real delivery API's apiKeyGuard (missing/wrong key -> 401).
 */
export function installFakeServer(opts: FakeServerOptions): FakeServerHandle {
  const requests: CapturedRequest[] = [];

  const stub = vi.fn(async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
    const url = toUrl(input);
    const headers = toHeaders(input, init);
    requests.push({ url, headers });

    if (!url.startsWith(`${opts.endpoint}/v1/snapshot`)) {
      throw new Error(`fakeServer: no script for ${url}`);
    }

    const authHeader = headers.get("authorization");
    if (authHeader !== `Bearer ${opts.apiKey}`) {
      return jsonResponse(401, { error: "authentication required" });
    }

    return jsonResponse(opts.snapshot.status, opts.snapshot.body ?? { error: "no values set" });
  });

  globalThis.fetch = stub as unknown as typeof fetch;

  return {
    requests,
    restore() {
      globalThis.fetch = realFetch;
    },
  };
}

function toUrl(input: RequestInfo | URL): string {
  if (typeof input === "string") return input;
  if (input instanceof URL) return input.toString();
  return input.url;
}

function toHeaders(input: RequestInfo | URL, init?: RequestInit): Headers {
  if (init?.headers) return new Headers(init.headers);
  if (typeof input !== "string" && !(input instanceof URL) && input.headers) return new Headers(input.headers);
  return new Headers();
}

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" },
  });
}
