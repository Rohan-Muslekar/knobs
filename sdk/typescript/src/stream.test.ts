import { afterEach, describe, expect, it, vi } from "vitest";
import { openStream } from "./stream.js";
import type { Delta, Snapshot } from "./types.js";
import { createControllableStream, type ControllableStream } from "./test/controllableStream.js";

const ENDPOINT = "https://knobs.example.com";
const API_KEY = "test-api-key";

const realFetch = globalThis.fetch;

afterEach(() => {
  globalThis.fetch = realFetch;
  vi.restoreAllMocks();
});

interface CapturedRequest {
  url: string;
  headers: Headers;
}

/** Stubs `globalThis.fetch` to return `controllable.stream` as the response body, capturing requests. */
function stubFetchWithStream(controllable: ControllableStream): CapturedRequest[] {
  const requests: CapturedRequest[] = [];
  const stub = vi.fn(async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
    const url = typeof input === "string" ? input : input.toString();
    requests.push({ url, headers: new Headers(init?.headers) });
    return new Response(controllable.stream, {
      status: 200,
      headers: { "content-type": "text/event-stream" },
    });
  });
  globalThis.fetch = stub as unknown as typeof fetch;
  return requests;
}

/** Waits one macrotask tick so the stream's async read loop can process pushed bytes. */
function flush(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

describe("openStream", () => {
  it("requests the stream endpoint with since + deltas=1 + bearer + accept headers (useDeltas defaults true)", async () => {
    const controllable = createControllableStream();
    const requests = stubFetchWithStream(controllable);

    const cancel = openStream(
      { endpoint: ENDPOINT, apiKey: API_KEY, environment: "prod" },
      42,
      vi.fn(),
      vi.fn(),
      vi.fn(),
    );
    await flush();

    expect(requests).toHaveLength(1);
    expect(requests[0].url).toBe(`${ENDPOINT}/v1/stream?since=42&deltas=1`);
    expect(requests[0].headers.get("authorization")).toBe(`Bearer ${API_KEY}`);
    expect(requests[0].headers.get("accept")).toBe("text/event-stream");

    cancel();
    controllable.close();
  });

  it("omits deltas=1 when useDeltas is explicitly false", async () => {
    const controllable = createControllableStream();
    const requests = stubFetchWithStream(controllable);

    const cancel = openStream(
      { endpoint: ENDPOINT, apiKey: API_KEY, environment: "prod", useDeltas: false },
      42,
      vi.fn(),
      vi.fn(),
      vi.fn(),
    );
    await flush();

    expect(requests[0].url).toBe(`${ENDPOINT}/v1/stream?since=42`);

    cancel();
    controllable.close();
  });

  it("parses a data: frame with type:\"snapshot\" into onSnapshot (explicit type, same as bare)", async () => {
    const controllable = createControllableStream();
    stubFetchWithStream(controllable);
    const onSnapshot = vi.fn();
    const onDelta = vi.fn();
    const onError = vi.fn();

    const cancel = openStream(
      { endpoint: ENDPOINT, apiKey: API_KEY, environment: "prod" },
      0,
      onSnapshot,
      onDelta,
      onError,
    );

    const snap = { type: "snapshot", version: 1, revision: 1, schemaHash: "h", values: { a: 1 } };
    controllable.push(`data: ${JSON.stringify(snap)}\n\n`);
    await flush();

    expect(onSnapshot).toHaveBeenCalledTimes(1);
    expect(onSnapshot).toHaveBeenCalledWith(snap);
    expect(onDelta).not.toHaveBeenCalled();
    expect(onError).not.toHaveBeenCalled();

    cancel();
    controllable.close();
  });

  it("parses a data: frame with type:\"delta\" into onDelta, not onSnapshot", async () => {
    const controllable = createControllableStream();
    stubFetchWithStream(controllable);
    const onSnapshot = vi.fn();
    const onDelta = vi.fn();
    const onError = vi.fn();

    const cancel = openStream(
      { endpoint: ENDPOINT, apiKey: API_KEY, environment: "prod" },
      0,
      onSnapshot,
      onDelta,
      onError,
    );

    const delta: Delta = {
      type: "delta",
      version: 1,
      revision: 2,
      from: 1,
      schemaHash: "h",
      values: { set: { a: 2 } },
    };
    controllable.push(`data: ${JSON.stringify(delta)}\n\n`);
    await flush();

    expect(onDelta).toHaveBeenCalledTimes(1);
    expect(onDelta).toHaveBeenCalledWith(delta);
    expect(onSnapshot).not.toHaveBeenCalled();
    expect(onError).not.toHaveBeenCalled();

    cancel();
    controllable.close();
  });

  it("parses each data: frame into onSnapshot", async () => {
    const controllable = createControllableStream();
    stubFetchWithStream(controllable);
    const onSnapshot = vi.fn();
    const onError = vi.fn();

    const cancel = openStream({ endpoint: ENDPOINT, apiKey: API_KEY, environment: "prod" }, 0, onSnapshot, vi.fn(), onError);

    const snap: Snapshot = { version: 1, revision: 1, schemaHash: "h", values: { a: 1 } };
    controllable.push(`data: ${JSON.stringify(snap)}\n\n`);
    await flush();

    expect(onSnapshot).toHaveBeenCalledTimes(1);
    expect(onSnapshot).toHaveBeenCalledWith(snap);
    expect(onError).not.toHaveBeenCalled();

    cancel();
    controllable.close();
  });

  it("ignores :-prefixed heartbeat comment frames", async () => {
    const controllable = createControllableStream();
    stubFetchWithStream(controllable);
    const onSnapshot = vi.fn();
    const onError = vi.fn();

    const cancel = openStream({ endpoint: ENDPOINT, apiKey: API_KEY, environment: "prod" }, 0, onSnapshot, vi.fn(), onError);

    controllable.push(": heartbeat\n\n");
    await flush();

    expect(onSnapshot).not.toHaveBeenCalled();
    expect(onError).not.toHaveBeenCalled();

    // a real snapshot after the heartbeat still comes through on the same connection
    const snap: Snapshot = { version: 2, revision: 2, schemaHash: "h", values: { b: 2 } };
    controllable.push(`data: ${JSON.stringify(snap)}\n\n`);
    await flush();

    expect(onSnapshot).toHaveBeenCalledTimes(1);
    expect(onSnapshot).toHaveBeenCalledWith(snap);

    cancel();
    controllable.close();
  });

  it("reassembles a data: frame split across two chunk boundaries", async () => {
    const controllable = createControllableStream();
    stubFetchWithStream(controllable);
    const onSnapshot = vi.fn();
    const onError = vi.fn();

    const cancel = openStream({ endpoint: ENDPOINT, apiKey: API_KEY, environment: "prod" }, 0, onSnapshot, vi.fn(), onError);

    const snap: Snapshot = { version: 3, revision: 3, schemaHash: "h", values: { split: "frame" } };
    const full = `data: ${JSON.stringify(snap)}\n\n`;
    const mid = Math.floor(full.length / 2);

    controllable.push(full.slice(0, mid));
    await flush();
    expect(onSnapshot).not.toHaveBeenCalled();

    controllable.push(full.slice(mid));
    await flush();

    expect(onSnapshot).toHaveBeenCalledTimes(1);
    expect(onSnapshot).toHaveBeenCalledWith(snap);
    expect(onError).not.toHaveBeenCalled();

    cancel();
    controllable.close();
  });

  it("cancel() aborts the stream — subsequent frames are not delivered", async () => {
    const controllable = createControllableStream();
    stubFetchWithStream(controllable);
    const onSnapshot = vi.fn();
    const onError = vi.fn();

    const cancel = openStream({ endpoint: ENDPOINT, apiKey: API_KEY, environment: "prod" }, 0, onSnapshot, vi.fn(), onError);

    const before: Snapshot = { version: 1, revision: 1, schemaHash: "h", values: { before: true } };
    controllable.push(`data: ${JSON.stringify(before)}\n\n`);
    await flush();
    expect(onSnapshot).toHaveBeenCalledTimes(1);

    cancel();
    await flush();

    const after: Snapshot = { version: 2, revision: 2, schemaHash: "h", values: { after: true } };
    controllable.push(`data: ${JSON.stringify(after)}\n\n`);
    await flush();

    expect(onSnapshot).toHaveBeenCalledTimes(1); // still just the "before" call
    expect(onError).not.toHaveBeenCalled();
  });

  it("calls onError when the fetch itself rejects", async () => {
    const stub = vi.fn(async () => {
      throw new Error("network down");
    });
    globalThis.fetch = stub as unknown as typeof fetch;
    const onSnapshot = vi.fn();
    const onError = vi.fn();

    const cancel = openStream({ endpoint: ENDPOINT, apiKey: API_KEY, environment: "prod" }, 0, onSnapshot, vi.fn(), onError);
    await flush();

    expect(onError).toHaveBeenCalledTimes(1);
    expect(onSnapshot).not.toHaveBeenCalled();

    cancel();
  });

  it("calls onError exactly once for a malformed data: frame and stops reading (no further onSnapshot)", async () => {
    const controllable = createControllableStream();
    stubFetchWithStream(controllable);
    const onSnapshot = vi.fn();
    const onError = vi.fn();

    const cancel = openStream({ endpoint: ENDPOINT, apiKey: API_KEY, environment: "prod" }, 0, onSnapshot, vi.fn(), onError);

    controllable.push("data: {not valid json\n\n");
    await flush();

    expect(onError).toHaveBeenCalledTimes(1);
    expect(onSnapshot).not.toHaveBeenCalled();

    // A well-formed frame arriving after the parse failure must not be delivered — the
    // connection is treated as done once onError fires, matching every other error path.
    const snap: Snapshot = { version: 1, revision: 1, schemaHash: "h", values: { a: 1 } };
    controllable.push(`data: ${JSON.stringify(snap)}\n\n`);
    await flush();

    expect(onSnapshot).not.toHaveBeenCalled();
    expect(onError).toHaveBeenCalledTimes(1);

    cancel();
    controllable.close();
  });

  it("aborts the underlying connection on a malformed frame, before onError fires", async () => {
    const controllable = createControllableStream();
    stubFetchWithStream(controllable);
    const onSnapshot = vi.fn();
    const callOrder: string[] = [];
    const onError = vi.fn(() => {
      // Recorded from inside the onError callback: proves the abort already happened by
      // the time onError is invoked, not just "eventually" — the underlying connection is
      // released on this terminal path, same as the cancel()-driven one.
      callOrder.push(controllable.cancelled ? "aborted-then-onError" : "onError-before-abort");
    });

    const cancel = openStream({ endpoint: ENDPOINT, apiKey: API_KEY, environment: "prod" }, 0, onSnapshot, vi.fn(), onError);

    expect(controllable.cancelled).toBe(false);

    controllable.push("data: {not valid json\n\n");
    await flush();

    expect(onError).toHaveBeenCalledTimes(1);
    expect(controllable.cancelled).toBe(true);
    expect(callOrder).toEqual(["aborted-then-onError"]);

    cancel();
    controllable.close();
  });

  it("calls onError when the stream closes (server ended the connection)", async () => {
    const controllable = createControllableStream();
    stubFetchWithStream(controllable);
    const onSnapshot = vi.fn();
    const onError = vi.fn();

    const cancel = openStream({ endpoint: ENDPOINT, apiKey: API_KEY, environment: "prod" }, 0, onSnapshot, vi.fn(), onError);
    controllable.close();
    await flush();

    expect(onError).toHaveBeenCalledTimes(1);
    expect(onSnapshot).not.toHaveBeenCalled();

    cancel();
  });
});
