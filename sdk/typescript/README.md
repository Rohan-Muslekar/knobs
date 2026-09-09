# @knobs/sdk

TypeScript SDK for [Knobs](../../) — live, in-memory config reads over the delivery API. Fetches the
initial snapshot, keeps it live via SSE with poll fallback, and exposes zero-network `get`/`getAll` reads.

## Install

```bash
npm install @knobs/sdk
```

If you plan to use the OpenFeature provider, also install the peer dependency:

```bash
npm install @openfeature/server-sdk
```

## Usage

```ts
import { createClient } from "@knobs/sdk";

const client = createClient({
  endpoint: "https://knobs.example.com",
  apiKey: process.env.KNOBS_API_KEY!,
  environment: "prod",
});

await client.ready(); // resolves once the first snapshot load has settled

client.get<number>("maxRetries"); // single key, no network call
client.getAll(); // full resolved config, no network call

const unsubscribe = client.onChange((values) => {
  // fires whenever a newer snapshot is applied (stream, reconnect, or poll fallback) —
  // NOT on the initial load, since ready()/getAll() already deliver that baseline.
  console.log("config updated", values);
});

// ... later, e.g. on process shutdown
unsubscribe();
client.close(); // stops streaming/reconnect/poll — safe to call more than once
```

### `createClient(opts)`

| Option               | Required | Description                                                                 |
| --------------------- | -------- | ----------------------------------------------------------------------------- |
| `endpoint`            | yes      | Base URL of the Knobs delivery API.                                          |
| `apiKey`              | yes      | Sent as `Authorization: Bearer <apiKey>`. The environment is implied by this key. |
| `environment`         | yes      | Informational; the server resolves the environment from the API key.        |
| `pollIntervalMs`      | no       | Slow-poll fallback interval, belt-and-suspenders alongside the SSE stream. Default `60000`. |
| `expectedSchemaHash`  | no       | The `schemaHash` your generated code was built against. A mismatch logs a `console.warn` (not a throw) — see codegen below. |

### `KnobsClient`

- `ready(): Promise<void>` — resolves once the first snapshot load attempt has settled (success or failure; a failed load falls back to an empty snapshot rather than rejecting).
- `get<T>(key: string): T | undefined` — reads one key from the in-memory snapshot.
- `getAll(): Record<string, unknown>` — reads the full in-memory snapshot.
- `onChange(cb): () => void` — registers a listener fired on every later snapshot swap; returns an unsubscribe function.
- `close(): void` — stops streaming, reconnect, and polling. Idempotent.

Updates are gated by the snapshot's `revision`, not `version`: a snapshot only replaces the current one if its
revision is strictly higher, so a stale replay is ignored and a rollback (higher revision, lower version) still
applies.

## Codegen: typed config with `knobs gen`

Rather than reading untyped `getAll()` values by string key, generate a typed accessor from your project's
schema with the `knobs` CLI:

```bash
knobs gen --lang ts --endpoint https://knobs.example.com --api-key <api-key> --out src/knobs.gen.ts
```

This fetches your project's schema (`GET {endpoint}/v1/schema`, authenticated with `--api-key`) and emits a
`knobs.gen.ts` with:

- an `AppConfig` interface — one member per schema field, typed per the field's schema type;
- `export const schemaHash` — pass this as `expectedSchemaHash` so a drifted server schema is flagged;
- `export function typed(client): AppConfig` — a compile-checked narrowing of `client.getAll()`.

Regenerate whenever the schema changes; `knobs.gen.ts` is generated output ("DO NOT EDIT" is stamped at the top) —
commit it like any other generated artifact, or regenerate it in CI.

```ts
import { createClient } from "@knobs/sdk";
import { typed, schemaHash } from "./knobs.gen.js";

const client = createClient({
  endpoint: "https://knobs.example.com",
  apiKey: process.env.KNOBS_API_KEY!,
  environment: "prod",
  expectedSchemaHash: schemaHash,
});
await client.ready();

const config = typed(client); // AppConfig — compile-checked field access
config.maxRetries; // number, not `unknown`
```

## OpenFeature

`@knobs/sdk` ships an OpenFeature `Provider` backed by a `KnobsClient`'s in-memory snapshot — resolution reads
are synchronous and zero-network, since the client already holds the live snapshot in memory.

```ts
import { OpenFeature } from "@openfeature/server-sdk";
import { createClient, KnobsProvider } from "@knobs/sdk";

const client = createClient({ endpoint: "https://knobs.example.com", apiKey: "...", environment: "prod" });

await OpenFeature.setProviderAndWait(new KnobsProvider(client));

const flags = OpenFeature.getClient();
const enabled = await flags.getBooleanValue("featureX", false);
```

A missing key or a runtime type mismatch against the requested flag type both fall back to the caller-supplied
default (`reason: "ERROR"` / `errorCode: TYPE_MISMATCH` on a mismatch, per the OpenFeature spec).

## Development

```bash
npm test    # vitest, incl. the e2e lifecycle test (fake server, no real network)
npm run build   # tsup -> dist/ (ESM + CJS + .d.ts)
```
