import { afterEach, describe, expect, it, vi } from "vitest";
import { createClient } from "./client.js";
import type { Snapshot } from "./types.js";
import { installFakeServer, type FakeServerHandle } from "./test/fakeServer.js";
import { createControllableStream, type ControllableStream } from "./test/controllableStream.js";

const ENDPOINT = "https://knobs.example.com";
const API_KEY = "e2e-api-key";

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

// Mirrors the shape `knobs gen --lang ts` would emit for a schema with these fields
// (see internal/codegen/testdata/expected.gen.ts) — standing in for a project's real
// knobs.gen.ts so this test can exercise the same typed()-over-getAll() pattern a
// generated file gives a consumer, without this SDK package depending on the Go
// codegen's golden fixture.
interface AppConfig {
  maxRetries: number;
  featureX: boolean;
}

function typed(client: { getAll(): Record<string, unknown> }): AppConfig {
  return client.getAll() as unknown as AppConfig;
}

describe("SDK e2e: createClient -> ready -> stream update -> close", () => {
  it("carries a client through its whole lifecycle in one flow", async () => {
    const initial: Snapshot = {
      version: 1,
      revision: 1,
      schemaHash: "e2e-hash",
      values: { maxRetries: 3, featureX: false },
    };
    const controllable = createControllableStream();
    server = installFakeServer({
      endpoint: ENDPOINT,
      apiKey: API_KEY,
      snapshot: { status: 200, body: initial },
      stream: controllable,
    });

    // createClient -> ready(): the first snapshot load settles before ready() resolves.
    const onChange = vi.fn();
    const client = createClient({ endpoint: ENDPOINT, apiKey: API_KEY, environment: "prod" });
    const unsubscribe = client.onChange(onChange);
    await client.ready();

    // Initial getAll() values are present, and readable through the generated typed()
    // accessor a real consumer would import from knobs.gen.ts.
    expect(client.getAll()).toEqual({ maxRetries: 3, featureX: false });
    expect(client.get<number>("maxRetries")).toBe(3);
    const config = typed(client);
    expect(config.maxRetries).toBe(3);
    expect(config.featureX).toBe(false);
    // ready()/getAll() already deliver the baseline synchronously, so the initial load
    // itself does not fire onChange (see client.ts's documented choice).
    expect(onChange).not.toHaveBeenCalled();

    // A streamed higher-revision snapshot updates getAll() and fires onChange.
    const updated: Snapshot = {
      version: 2,
      revision: 2,
      schemaHash: "e2e-hash",
      values: { maxRetries: 10, featureX: true },
    };
    pushSnapshot(controllable, updated);
    await flush();

    expect(client.getAll()).toEqual({ maxRetries: 10, featureX: true });
    expect(typed(client).maxRetries).toBe(10);
    expect(onChange).toHaveBeenCalledTimes(1);
    expect(onChange).toHaveBeenCalledWith({ maxRetries: 10, featureX: true });

    // close() stops further updates: a snapshot pushed after close() is never applied.
    client.close();

    const afterClose: Snapshot = {
      version: 3,
      revision: 3,
      schemaHash: "e2e-hash",
      values: { maxRetries: 999, featureX: false },
    };
    pushSnapshot(controllable, afterClose);
    await flush();

    expect(client.getAll()).toEqual({ maxRetries: 10, featureX: true });
    expect(onChange).toHaveBeenCalledTimes(1);

    unsubscribe();
    controllable.close();
  });
});
