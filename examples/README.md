# Knobs — consumer reference examples

Short, copy-pasteable examples of consuming a Knobs config server from an app.

| Example | What it shows |
|---|---|
| [`typescript/`](./typescript) | Plain TS: `createClient` → in-memory typed reads + live `onChange`. Untyped and codegen-typed variants. |
| [`nestjs/`](./nestjs) | A `KnobsModule` wiring the client as a DI provider with `onModuleInit`/`onModuleDestroy` lifecycle, plus optional OpenFeature. |
| [`golang/`](./golang) | A minimal stdlib client hitting the delivery API directly (`GET /v1/snapshot` + SSE `GET /v1/stream`, Bearer) — kept as a "how it works under the hood" illustration. For production Go, use the real [`sdk/go`](../sdk/go) module instead. |

## The model in one paragraph

Knobs is a **thick-SDK** config server: the client fetches the whole config
**snapshot** for one environment over HTTP (Bearer, an env-scoped read API key),
holds it in memory, and every read is a zero-network lookup. It then holds an
**SSE stream** open; the server pushes a fresh snapshot whenever the environment's
current version changes, and the client swaps it in atomically. Dedup/reconnect
use the monotonic `revision` (never the user-facing `version`, so a rollback —
which carries a higher `revision` but a lower `version` — still applies).

## Getting an endpoint + API key

1. Run the server (`make build && ./bin/server` with `DATABASE_URL`, `JWT_SECRET`,
   `ADMIN_EMAIL`, `ADMIN_PASSWORD` set), open the admin UI, log in.
2. Create a project → define a schema → create an environment → save values.
3. In that environment, create a **read API key** (shown once — copy it).
4. Point the examples at `endpoint` (the server URL) + that `apiKey`.

For the typed TS/NestJS flow, generate the types first:

```
knobs gen --lang ts --endpoint <url> --api-key <key> --out src/knobs.gen.ts
```
