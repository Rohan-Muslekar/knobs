import { afterEach, describe, expect, it } from "vitest";
import { OpenFeature } from "@openfeature/server-sdk";
import { createClient } from "./client.js";
import type { Snapshot } from "./types.js";
import { KnobsProvider } from "./openfeature.js";
import { installFakeServer, type FakeServerHandle } from "./test/fakeServer.js";

const ENDPOINT = "https://knobs.example.com";
const API_KEY = "test-api-key";

let server: FakeServerHandle | undefined;

afterEach(async () => {
  await OpenFeature.clearProviders();
  server?.restore();
  server = undefined;
});

describe("KnobsProvider", () => {
  it("resolves boolean/number/string/object values from the served snapshot, and falls back to the default on a missing key or type mismatch", async () => {
    const snapshot: Snapshot = {
      version: 1,
      revision: 1,
      schemaHash: "h",
      values: { featureX: true, maxRetries: 3, name: "x", cfg: { a: 1 } },
    };
    server = installFakeServer({ endpoint: ENDPOINT, apiKey: API_KEY, snapshot: { status: 200, body: snapshot } });

    const client = createClient({ endpoint: ENDPOINT, apiKey: API_KEY, environment: "prod" });
    const provider = new KnobsProvider(client);

    await OpenFeature.setProviderAndWait(provider);
    const ofClient = OpenFeature.getClient();

    expect(await ofClient.getBooleanValue("featureX", false)).toBe(true);
    expect(await ofClient.getNumberValue("maxRetries", 0)).toBe(3);
    expect(await ofClient.getStringValue("name", "")).toBe("x");
    expect(await ofClient.getObjectValue("cfg", {})).toEqual({ a: 1 });

    // Missing key -> the passed default.
    expect(await ofClient.getBooleanValue("doesNotExist", true)).toBe(true);
    expect(await ofClient.getStringValue("doesNotExist", "fallback")).toBe("fallback");

    // Type mismatch -> the passed default (not the mistyped stored value).
    expect(await ofClient.getBooleanValue("maxRetries", false)).toBe(false);

    client.close();
  });
});
