import { afterEach, describe, expect, it, vi } from "vitest";
import { createClient } from "./client.js";
import type { Delta, Snapshot } from "./types.js";
import { installFakeServer, type FakeServerHandle } from "./test/fakeServer.js";
import { createControllableStream, type ControllableStream } from "./test/controllableStream.js";

const ENDPOINT = "https://knobs.example.com";
const API_KEY = "test-api-key";

let server: FakeServerHandle | undefined;

afterEach(() => {
  server?.restore();
  server = undefined;
  vi.restoreAllMocks();
});

/** Waits one macrotask tick so a client's async stream-read loop can process pushed bytes. */
function flush(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

function pushSnapshot(controllable: ControllableStream, snapshot: Snapshot): void {
  controllable.push(`data: ${JSON.stringify(snapshot)}\n\n`);
}

function pushDelta(controllable: ControllableStream, delta: Delta): void {
  controllable.push(`data: ${JSON.stringify(delta)}\n\n`);
}

describe("createClient", () => {
  it("loads the served snapshot and exposes it via getAll/get", async () => {
    const snapshot: Snapshot = {
      version: 3,
      revision: 42,
      schemaHash: "abc123",
      values: { maxRetries: 5, featureX: true },
    };
    server = installFakeServer({ endpoint: ENDPOINT, apiKey: API_KEY, snapshot: { status: 200, body: snapshot } });

    const client = createClient({ endpoint: ENDPOINT, apiKey: API_KEY, environment: "prod" });
    await client.ready();

    expect(client.getAll()).toEqual({ maxRetries: 5, featureX: true });
    expect(client.get("maxRetries")).toBe(5);
    expect(client.get("missingKey")).toBeUndefined();

    client.close();
  });

  it("sends the Authorization: Bearer header on the snapshot request", async () => {
    const snapshot: Snapshot = { version: 1, revision: 1, schemaHash: "h", values: {} };
    server = installFakeServer({ endpoint: ENDPOINT, apiKey: API_KEY, snapshot: { status: 200, body: snapshot } });

    const client = createClient({ endpoint: ENDPOINT, apiKey: API_KEY, environment: "prod" });
    await client.ready();

    // The client also opens the live stream right after the initial snapshot loads (Task 3)
    // — this test is about the snapshot request specifically, so it checks that one request
    // rather than the total count.
    const snapshotRequest = server.requests.find((r) => r.url === `${ENDPOINT}/v1/snapshot`);
    expect(snapshotRequest?.headers.get("authorization")).toBe(`Bearer ${API_KEY}`);

    client.close();
  });

  it("treats a 404 (no values set) as an empty snapshot", async () => {
    server = installFakeServer({ endpoint: ENDPOINT, apiKey: API_KEY, snapshot: { status: 404 } });

    const client = createClient({ endpoint: ENDPOINT, apiKey: API_KEY, environment: "prod" });
    await client.ready();

    expect(client.getAll()).toEqual({});
    expect(client.get("anything")).toBeUndefined();

    client.close();
  });

  it("preserves a path prefix in the endpoint when building the snapshot URL", async () => {
    const prefixedEndpoint = `${ENDPOINT}/knobs`;
    const snapshot: Snapshot = { version: 1, revision: 1, schemaHash: "h", values: { a: 1 } };
    server = installFakeServer({
      endpoint: prefixedEndpoint,
      apiKey: API_KEY,
      snapshot: { status: 200, body: snapshot },
    });

    const client = createClient({ endpoint: prefixedEndpoint, apiKey: API_KEY, environment: "prod" });
    await client.ready();

    // Same rationale as the header test above: check the snapshot request specifically
    // rather than the total request count, since the client also opens the stream (Task 3).
    const snapshotRequest = server.requests.find((r) => r.url === `${prefixedEndpoint}/v1/snapshot`);
    expect(snapshotRequest).toBeDefined();
    expect(client.getAll()).toEqual({ a: 1 });

    client.close();
  });

  it("does not warn about schemaHash drift when the snapshot is the empty/404 sentinel", async () => {
    server = installFakeServer({ endpoint: ENDPOINT, apiKey: API_KEY, snapshot: { status: 404 } });
    const warnSpy = vi.spyOn(console, "warn").mockImplementation(() => {});

    const client = createClient({
      endpoint: ENDPOINT,
      apiKey: API_KEY,
      environment: "prod",
      expectedSchemaHash: "expected-hash",
    });
    await client.ready();

    expect(warnSpy).not.toHaveBeenCalled();
    expect(client.getAll()).toEqual({});

    client.close();
  });

  it("warns (does not throw) when expectedSchemaHash differs from the served snapshot's hash", async () => {
    const snapshot: Snapshot = { version: 1, revision: 1, schemaHash: "actual-hash", values: { a: 1 } };
    server = installFakeServer({ endpoint: ENDPOINT, apiKey: API_KEY, snapshot: { status: 200, body: snapshot } });
    const warnSpy = vi.spyOn(console, "warn").mockImplementation(() => {});

    const client = createClient({
      endpoint: ENDPOINT,
      apiKey: API_KEY,
      environment: "prod",
      expectedSchemaHash: "expected-hash",
    });
    await expect(client.ready()).resolves.toBeUndefined();

    expect(warnSpy).toHaveBeenCalledTimes(1);
    expect(warnSpy.mock.calls[0][0]).toContain("schemaHash");

    client.close();
  });

  it("does not warn when expectedSchemaHash matches", async () => {
    const snapshot: Snapshot = { version: 1, revision: 1, schemaHash: "matching-hash", values: {} };
    server = installFakeServer({ endpoint: ENDPOINT, apiKey: API_KEY, snapshot: { status: 200, body: snapshot } });
    const warnSpy = vi.spyOn(console, "warn").mockImplementation(() => {});

    const client = createClient({
      endpoint: ENDPOINT,
      apiKey: API_KEY,
      environment: "prod",
      expectedSchemaHash: "matching-hash",
    });
    await client.ready();

    expect(warnSpy).not.toHaveBeenCalled();

    client.close();
  });

  it("getAll() returns a copy — mutating it does not corrupt the SDK's in-memory snapshot", async () => {
    const snapshot: Snapshot = { version: 1, revision: 1, schemaHash: "h", values: { a: 1 } };
    server = installFakeServer({ endpoint: ENDPOINT, apiKey: API_KEY, snapshot: { status: 200, body: snapshot } });

    const client = createClient({ endpoint: ENDPOINT, apiKey: API_KEY, environment: "prod" });
    await client.ready();

    const first = client.getAll();
    first.a = "mutated";
    (first as Record<string, unknown>).injected = true;

    expect(client.getAll()).toEqual({ a: 1 });
    expect(client.get("a")).toBe(1);
    expect(client.getAll()).not.toBe(first);

    client.close();
  });

  it("onChange registers a listener and returns an unsubscribe function (no fire on first load)", async () => {
    const snapshot: Snapshot = { version: 1, revision: 1, schemaHash: "h", values: { a: 1 } };
    server = installFakeServer({ endpoint: ENDPOINT, apiKey: API_KEY, snapshot: { status: 200, body: snapshot } });

    const client = createClient({ endpoint: ENDPOINT, apiKey: API_KEY, environment: "prod" });
    const cb = vi.fn();
    const unsubscribe = client.onChange(cb);
    await client.ready();

    // Documented choice: the initial load does not fire onChange (ready()/getAll() deliver
    // the baseline; onChange fires only on later swaps, wired up fully in Task 3's streaming).
    expect(cb).not.toHaveBeenCalled();
    expect(typeof unsubscribe).toBe("function");

    unsubscribe();
    client.close();
  });
});

describe("live streaming", () => {
  it("applies a streamed snapshot with a higher revision: updates getAll and fires onChange", async () => {
    const initial: Snapshot = { version: 1, revision: 1, schemaHash: "h", values: { a: 1 } };
    const controllable = createControllableStream();
    server = installFakeServer({
      endpoint: ENDPOINT,
      apiKey: API_KEY,
      snapshot: { status: 200, body: initial },
      stream: controllable,
    });

    const onChange = vi.fn();
    const client = createClient({ endpoint: ENDPOINT, apiKey: API_KEY, environment: "prod" });
    client.onChange(onChange);
    await client.ready();

    const higher: Snapshot = { version: 2, revision: 2, schemaHash: "h", values: { a: 2 } };
    pushSnapshot(controllable, higher);
    await flush();

    expect(client.getAll()).toEqual({ a: 2 });
    expect(onChange).toHaveBeenCalledTimes(1);
    expect(onChange).toHaveBeenCalledWith({ a: 2 });

    client.close();
    controllable.close();
  });

  it("ignores a streamed snapshot with an equal or lower revision (stale replay, no onChange)", async () => {
    const initial: Snapshot = { version: 5, revision: 10, schemaHash: "h", values: { a: "current" } };
    const controllable = createControllableStream();
    server = installFakeServer({
      endpoint: ENDPOINT,
      apiKey: API_KEY,
      snapshot: { status: 200, body: initial },
      stream: controllable,
    });

    const onChange = vi.fn();
    const client = createClient({ endpoint: ENDPOINT, apiKey: API_KEY, environment: "prod" });
    client.onChange(onChange);
    await client.ready();

    // Equal revision — a benign duplicate (e.g. reconnect resent the same frame).
    pushSnapshot(controllable, { version: 6, revision: 10, schemaHash: "h", values: { a: "equal-replay" } });
    await flush();
    expect(client.getAll()).toEqual({ a: "current" });
    expect(onChange).not.toHaveBeenCalled();

    // Lower revision — a stale replay.
    pushSnapshot(controllable, { version: 7, revision: 9, schemaHash: "h", values: { a: "stale-replay" } });
    await flush();
    expect(client.getAll()).toEqual({ a: "current" });
    expect(onChange).not.toHaveBeenCalled();

    client.close();
    controllable.close();
  });

  it("applies a rollback: a HIGHER revision with a LOWER version still applies (revision, not version, gates)", async () => {
    const initial: Snapshot = { version: 10, revision: 5, schemaHash: "h", values: { a: "post-rollback-target" } };
    const controllable = createControllableStream();
    server = installFakeServer({
      endpoint: ENDPOINT,
      apiKey: API_KEY,
      snapshot: { status: 200, body: initial },
      stream: controllable,
    });

    const onChange = vi.fn();
    const client = createClient({ endpoint: ENDPOINT, apiKey: API_KEY, environment: "prod" });
    client.onChange(onChange);
    await client.ready();

    // A rollback: version drops from 10 back to 3, but revision (the delivery axis) still
    // only ever increases — 6 > 5 — so this must apply despite the lower version.
    const rollback: Snapshot = { version: 3, revision: 6, schemaHash: "h", values: { a: "rolled-back" } };
    pushSnapshot(controllable, rollback);
    await flush();

    expect(client.getAll()).toEqual({ a: "rolled-back" });
    expect(onChange).toHaveBeenCalledTimes(1);
    expect(onChange).toHaveBeenCalledWith({ a: "rolled-back" });

    client.close();
    controllable.close();
  });

  it("close() stops applying streamed updates", async () => {
    const initial: Snapshot = { version: 1, revision: 1, schemaHash: "h", values: { a: 1 } };
    const controllable = createControllableStream();
    server = installFakeServer({
      endpoint: ENDPOINT,
      apiKey: API_KEY,
      snapshot: { status: 200, body: initial },
      stream: controllable,
    });

    const onChange = vi.fn();
    const client = createClient({ endpoint: ENDPOINT, apiKey: API_KEY, environment: "prod" });
    client.onChange(onChange);
    await client.ready();

    client.close();

    const after: Snapshot = { version: 2, revision: 2, schemaHash: "h", values: { a: 2 } };
    pushSnapshot(controllable, after);
    await flush();

    expect(client.getAll()).toEqual({ a: 1 });
    expect(onChange).not.toHaveBeenCalled();

    controllable.close();
  });

  it("slow-polls fetchSnapshot as a fallback and applies it via the same revision gate", async () => {
    const initial: Snapshot = { version: 1, revision: 1, schemaHash: "h", values: { a: 1 } };
    const polled: Snapshot = { version: 2, revision: 2, schemaHash: "h", values: { a: 2 } };
    const controllable = createControllableStream(); // stream stays open, idle — poll is the only source of updates here

    let snapshotCallCount = 0;
    const realFetch = globalThis.fetch;
    const stub = vi.fn(async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
      const url = typeof input === "string" ? input : input.toString();
      const headers = new Headers(init?.headers);
      if (headers.get("authorization") !== `Bearer ${API_KEY}`) {
        return new Response(JSON.stringify({ error: "authentication required" }), { status: 401 });
      }
      if (url.startsWith(`${ENDPOINT}/v1/stream`)) {
        return new Response(controllable.stream, { status: 200, headers: { "content-type": "text/event-stream" } });
      }
      if (url.startsWith(`${ENDPOINT}/v1/snapshot`)) {
        snapshotCallCount += 1;
        const body = snapshotCallCount === 1 ? initial : polled;
        return new Response(JSON.stringify(body), { status: 200, headers: { "content-type": "application/json" } });
      }
      throw new Error(`unexpected request: ${url}`);
    });
    globalThis.fetch = stub as unknown as typeof fetch;

    const onChange = vi.fn();
    const client = createClient({ endpoint: ENDPOINT, apiKey: API_KEY, environment: "prod", pollIntervalMs: 20 });
    client.onChange(onChange);
    await client.ready();
    expect(client.getAll()).toEqual({ a: 1 });

    // Bounded real-timer wait past a couple of 20ms poll intervals — no fake timers needed,
    // no hang risk (the wait is fixed and short).
    await new Promise((resolve) => setTimeout(resolve, 150));

    expect(snapshotCallCount).toBeGreaterThanOrEqual(2);
    expect(client.getAll()).toEqual({ a: 2 });
    expect(onChange).toHaveBeenCalledWith({ a: 2 });

    client.close();
    const countAtClose = snapshotCallCount;
    await new Promise((resolve) => setTimeout(resolve, 60));
    expect(snapshotCallCount).toBe(countAtClose); // poll timer was cleared by close()

    globalThis.fetch = realFetch;
    controllable.close();
  });

  it("passes onChange listeners a copy of the values — mutating it does not corrupt the snapshot", async () => {
    const initial: Snapshot = { version: 1, revision: 1, schemaHash: "h", values: { a: 1 } };
    const controllable = createControllableStream();
    server = installFakeServer({
      endpoint: ENDPOINT,
      apiKey: API_KEY,
      snapshot: { status: 200, body: initial },
      stream: controllable,
    });

    let received: Record<string, unknown> | undefined;
    const client = createClient({ endpoint: ENDPOINT, apiKey: API_KEY, environment: "prod" });
    client.onChange((values) => {
      received = values;
    });
    await client.ready();

    const higher: Snapshot = { version: 2, revision: 2, schemaHash: "h", values: { a: 2 } };
    pushSnapshot(controllable, higher);
    await flush();

    expect(received).toEqual({ a: 2 });
    received!.a = "mutated";
    expect(client.getAll()).toEqual({ a: 2 }); // mutating the delivered object left the snapshot intact

    client.close();
    controllable.close();
  });

  it("re-checks schemaHash drift on later swaps: warns once per distinct hash, not once per interval", async () => {
    const initial: Snapshot = { version: 1, revision: 1, schemaHash: "matching-hash", values: { a: 1 } };
    const controllable = createControllableStream();
    server = installFakeServer({
      endpoint: ENDPOINT,
      apiKey: API_KEY,
      snapshot: { status: 200, body: initial },
      stream: controllable,
    });
    const warnSpy = vi.spyOn(console, "warn").mockImplementation(() => {});

    const client = createClient({
      endpoint: ENDPOINT,
      apiKey: API_KEY,
      environment: "prod",
      expectedSchemaHash: "matching-hash",
    });
    await client.ready();

    // First load matched, so no warning yet.
    expect(warnSpy).not.toHaveBeenCalled();

    // A schema change arrives via the stream mid-process (e.g. a long-lived NestJS service) —
    // its schemaHash now differs from expectedSchemaHash. Must warn exactly once.
    const drifted: Snapshot = { version: 2, revision: 2, schemaHash: "drifted-hash", values: { a: 2 } };
    pushSnapshot(controllable, drifted);
    await flush();

    expect(warnSpy).toHaveBeenCalledTimes(1);
    expect(warnSpy.mock.calls[0][0]).toContain("schemaHash");

    // A later swap carrying the SAME drifted hash (e.g. the belt-and-suspenders poll
    // re-fetching the still-drifted schema, or a reconnect resend) must not warn again.
    const stillDrifted: Snapshot = { version: 2, revision: 3, schemaHash: "drifted-hash", values: { a: 3 } };
    pushSnapshot(controllable, stillDrifted);
    await flush();

    expect(warnSpy).toHaveBeenCalledTimes(1);

    client.close();
    controllable.close();
  });

  it("warns again if the schema drifts to a NEW distinct hash after an earlier drift warning", async () => {
    const initial: Snapshot = { version: 1, revision: 1, schemaHash: "actual-hash", values: { a: 1 } };
    const controllable = createControllableStream();
    server = installFakeServer({
      endpoint: ENDPOINT,
      apiKey: API_KEY,
      snapshot: { status: 200, body: initial },
      stream: controllable,
    });
    const warnSpy = vi.spyOn(console, "warn").mockImplementation(() => {});

    const client = createClient({
      endpoint: ENDPOINT,
      apiKey: API_KEY,
      environment: "prod",
      expectedSchemaHash: "expected-hash",
    });
    await client.ready();

    // First-load drift already warns once (existing behavior).
    expect(warnSpy).toHaveBeenCalledTimes(1);

    // A second swap with the SAME hash as the first-load drift must not double-warn.
    const sameHash: Snapshot = { version: 1, revision: 2, schemaHash: "actual-hash", values: { a: 2 } };
    pushSnapshot(controllable, sameHash);
    await flush();
    expect(warnSpy).toHaveBeenCalledTimes(1);

    // A THIRD swap with a newly different hash must warn again.
    const newHash: Snapshot = { version: 1, revision: 3, schemaHash: "yet-another-hash", values: { a: 3 } };
    pushSnapshot(controllable, newHash);
    await flush();
    expect(warnSpy).toHaveBeenCalledTimes(2);

    client.close();
    controllable.close();
  });
});

describe("delta streaming", () => {
  it("still applies a frame with type:\"snapshot\" (explicit type, same as a bare frame)", async () => {
    const initial: Snapshot = { version: 1, revision: 1, schemaHash: "h", values: { a: 1 } };
    const controllable = createControllableStream();
    server = installFakeServer({
      endpoint: ENDPOINT,
      apiKey: API_KEY,
      snapshot: { status: 200, body: initial },
      stream: controllable,
    });

    const client = createClient({ endpoint: ENDPOINT, apiKey: API_KEY, environment: "prod" });
    await client.ready();

    controllable.push(
      `data: ${JSON.stringify({ type: "snapshot", version: 2, revision: 2, schemaHash: "h", values: { a: 2 } })}\n\n`,
    );
    await flush();

    expect(client.getAll()).toEqual({ a: 2 });

    client.close();
    controllable.close();
  });

  it("applies a delta frame whose from matches current.revision: merges values and fires onChange", async () => {
    const initial: Snapshot = { version: 1, revision: 1, schemaHash: "h", values: { retries: 3, timeout: 30 } };
    const controllable = createControllableStream();
    server = installFakeServer({
      endpoint: ENDPOINT,
      apiKey: API_KEY,
      snapshot: { status: 200, body: initial },
      stream: controllable,
    });

    const onChange = vi.fn();
    const client = createClient({ endpoint: ENDPOINT, apiKey: API_KEY, environment: "prod" });
    client.onChange(onChange);
    await client.ready();

    const delta: Delta = {
      type: "delta",
      version: 1,
      revision: 2,
      from: 1,
      schemaHash: "h",
      values: { set: { retries: 5 }, remove: ["timeout"] },
    };
    pushDelta(controllable, delta);
    await flush();

    expect(client.getAll()).toEqual({ retries: 5 });
    expect(client.get("retries")).toBe(5);
    expect(client.get("timeout")).toBeUndefined();
    expect(onChange).toHaveBeenCalledTimes(1);
    expect(onChange).toHaveBeenCalledWith({ retries: 5 });

    client.close();
    controllable.close();
  });

  it("applies consecutive deltas by chaining off the revision each one produces", async () => {
    const initial: Snapshot = { version: 1, revision: 1, schemaHash: "h", values: { a: 1 } };
    const controllable = createControllableStream();
    server = installFakeServer({
      endpoint: ENDPOINT,
      apiKey: API_KEY,
      snapshot: { status: 200, body: initial },
      stream: controllable,
    });

    const client = createClient({ endpoint: ENDPOINT, apiKey: API_KEY, environment: "prod" });
    await client.ready();

    pushDelta(controllable, {
      type: "delta",
      version: 1,
      revision: 2,
      from: 1,
      schemaHash: "h",
      values: { set: { a: 2 } },
    });
    await flush();
    expect(client.getAll()).toEqual({ a: 2 });

    pushDelta(controllable, {
      type: "delta",
      version: 1,
      revision: 3,
      from: 2,
      schemaHash: "h",
      values: { set: { b: 3 } },
    });
    await flush();
    expect(client.getAll()).toEqual({ a: 2, b: 3 });

    client.close();
    controllable.close();
  });

  it("falls back to a full resync when a delta's from doesn't match current.revision", async () => {
    const initial: Snapshot = { version: 1, revision: 1, schemaHash: "h", values: { a: 1 } };
    const resynced: Snapshot = { version: 5, revision: 20, schemaHash: "h", values: { a: "resynced" } };
    const controllable = createControllableStream();

    let snapshotCallCount = 0;
    const stub = vi.fn(async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
      const url = typeof input === "string" ? input : input.toString();
      const headers = new Headers(init?.headers);
      if (headers.get("authorization") !== `Bearer ${API_KEY}`) {
        return new Response(JSON.stringify({ error: "authentication required" }), { status: 401 });
      }
      if (url.startsWith(`${ENDPOINT}/v1/stream`)) {
        return new Response(controllable.stream, { status: 200, headers: { "content-type": "text/event-stream" } });
      }
      if (url.startsWith(`${ENDPOINT}/v1/snapshot`)) {
        snapshotCallCount += 1;
        const body = snapshotCallCount === 1 ? initial : resynced;
        return new Response(JSON.stringify(body), { status: 200, headers: { "content-type": "application/json" } });
      }
      throw new Error(`unexpected request: ${url}`);
    });
    const realFetch = globalThis.fetch;
    globalThis.fetch = stub as unknown as typeof fetch;

    const onChange = vi.fn();
    const client = createClient({ endpoint: ENDPOINT, apiKey: API_KEY, environment: "prod" });
    client.onChange(onChange);
    await client.ready();
    expect(snapshotCallCount).toBe(1);

    // from=99 does not match current.revision (1) — a dropped-frame/race scenario the client
    // must not blindly apply on top of; it should refetch and apply a full snapshot instead.
    pushDelta(controllable, {
      type: "delta",
      version: 6,
      revision: 21,
      from: 99,
      schemaHash: "h",
      values: { set: { a: "should-not-apply-directly" } },
    });
    await flush();

    expect(snapshotCallCount).toBe(2); // the resync refetch
    expect(client.getAll()).toEqual({ a: "resynced" });
    expect(onChange).toHaveBeenCalledWith({ a: "resynced" });

    client.close();
    globalThis.fetch = realFetch;
    controllable.close();
  });
});
