// Plain TypeScript usage of @knobs/sdk — untyped reads.
//
// Run (after building the SDK and `npm install` here):
//   KNOBS_ENDPOINT=http://localhost:8080 KNOBS_API_KEY=knobs_xxx \
//     node --experimental-strip-types basic.ts
//
// (Node 22+ can run .ts directly with --experimental-strip-types; otherwise
//  compile with tsc/tsx first.)

import { createClient } from "@knobs/sdk";

const knobs = createClient({
  endpoint: process.env.KNOBS_ENDPOINT ?? "http://localhost:8080",
  apiKey: process.env.KNOBS_API_KEY ?? "",
  environment: process.env.KNOBS_ENV ?? "prod",
  // Optional: slow-poll fallback interval (ms) for missed SSE pushes.
  pollIntervalMs: 60_000,
});

// Wait for the first snapshot to load.
await knobs.ready();

// Reads are in-memory — zero network on the hot path.
console.log("all config:", knobs.getAll());
console.log("maxRetries:", knobs.get<number>("maxRetries"));
console.log("featureX:", knobs.get<boolean>("featureX"));

// React to live changes (server push over SSE). Returns an unsubscribe fn.
const unsubscribe = knobs.onChange((values) => {
  console.log("config changed →", values);
});

// Keep the process alive for a bit to observe live updates, then clean up.
await new Promise((r) => setTimeout(r, 30_000));
unsubscribe();
knobs.close(); // aborts the stream + clears timers — no leaks
