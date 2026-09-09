import { afterEach, describe, expect, it, vi } from "vitest";
import { createClient } from "./client.js";
import type { Snapshot } from "./types.js";
import { installFakeServer, type FakeServerHandle } from "./test/fakeServer.js";

const ENDPOINT = "https://knobs.example.com";
const API_KEY = "test-api-key";

let server: FakeServerHandle | undefined;

afterEach(() => {
  server?.restore();
  server = undefined;
  vi.restoreAllMocks();
});

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

    expect(server.requests).toHaveLength(1);
    expect(server.requests[0].url).toBe(`${ENDPOINT}/v1/snapshot`);
    expect(server.requests[0].headers.get("authorization")).toBe(`Bearer ${API_KEY}`);

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

    expect(server.requests).toHaveLength(1);
    expect(server.requests[0].url).toBe(`${prefixedEndpoint}/v1/snapshot`);
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
