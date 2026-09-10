# Knobs

**Typed remote config + feature flags you can self-host.** Define a *typed schema*
for your configuration once, edit values per environment in a web UI, and every
app picks up changes over a live stream — with generated, type-safe accessors in
your language. Think Firebase Remote Config / LaunchDarkly, but open, typed, and
yours to run.

- **Typed structured config** — a schema per project (string/int/float/bool/enum/duration/json,
  with min/max/pattern/enum constraints). Values are validated against it server-side.
- **Per-language codegen** — `knobs gen` emits a typed `Config` accessor for
  TypeScript or Go from your live schema.
- **Thick-SDK streaming** — the SDK fetches the whole config snapshot once, holds
  it in memory (every read is a zero-network lookup), and holds an SSE stream open
  so the server can push a fresh snapshot the instant a value changes.
- **Targeting & percentage rollout** — attach rules to a key (by caller attributes)
  and deterministic percentage rollouts. Rules ship in the snapshot and are
  evaluated *identically* in the server, the TS SDK, and the Go SDK.
- **Delta snapshots** — the stream can push only what changed (`?deltas=1`),
  falling back to full snapshots; old clients are unaffected.
- **Multi-tenant RBAC** — organizations own projects; users are members with a role
  (`viewer < editor < admin < owner`); every management action is authorized.
- **Versioned + auditable** — every value change is a new version; one-click
  rollback; an audit log per project.

## Stack

Go backend (chi, pgx, goose, Postgres) with an embedded React admin UI (Vite,
shadcn/ui, Tailwind, TanStack Query). SDKs for TypeScript and Go. One Postgres
database; no other infrastructure required.

## Quick start

**Prerequisites:** Go 1.26+, Node 20+ (to build the UI), a Postgres database.

```sh
# 1. Build everything (UI bundle + server + CLI) into ./bin
make build

# 2. Point at Postgres and set a session secret (>= 32 chars), seed an admin
export DATABASE_URL='postgres://user:pass@localhost:5432/knobs?sslmode=disable'
export JWT_SECRET='a-long-random-string-at-least-32-chars'
export ADMIN_EMAIL='you@example.com'
export ADMIN_PASSWORD='change-me'

# 3. Run — migrations apply automatically on startup, then the admin is seeded
./bin/server
```

Open `http://localhost:8080`, log in with the admin credentials, and:

1. Create a **project** → define its **schema** → create an **environment** → save **values**.
2. In that environment, create a **read API key** (shown once — copy it).
3. Point an SDK at the server URL + that API key.

### Configuration (environment variables)

| Variable | Purpose | Default | Required |
|---|---|---|---|
| `DATABASE_URL` | Postgres connection string | — | **yes** |
| `JWT_SECRET` | Signs the session cookie; must be ≥ 32 chars | — | **yes** |
| `LISTEN_ADDR` | HTTP listen address | `:8080` | no |
| `ADMIN_EMAIL` / `ADMIN_PASSWORD` | Seed the first admin user (owner of the `default` org) on an empty DB | — | no |
| `COOKIE_SECURE` | `Secure` flag on the session cookie; set `false` for local HTTP | `true` | no |
| `TRUST_PROXY` | Trust `X-Forwarded-For` for the login rate limiter (set behind a proxy) | `false` | no |

## Using it from an app

Both SDKs follow the same model: construct a client, `Ready()`/`ready()` it once
(loads the first snapshot and starts the live stream), then read config as plain
in-memory lookups. See [`examples/`](./examples) for TypeScript, NestJS, and Go.

**TypeScript** ([`sdk/typescript`](./sdk/typescript)):

```ts
import { createClient } from "@knobs/sdk";

const knobs = createClient({ endpoint: "https://knobs.example.com", apiKey: "…", environment: "production" });
await knobs.ready();
const maxRetries = knobs.get<number>("maxRetries");
knobs.onChange(() => { /* config changed */ });
```

**Go** ([`sdk/go`](./sdk/go)):

```go
c := knobs.New(knobs.Options{Endpoint: "https://knobs.example.com", APIKey: "…", Environment: "production"})
if err := c.Ready(ctx); err != nil { /* handle */ }
v, ok := c.Get("maxRetries")
```

### Typed accessors (codegen)

Generate a typed `Config` from your live schema so reads are compile-time checked:

```sh
# TypeScript
./bin/knobs gen --lang ts --endpoint https://knobs.example.com --api-key <key> --out src/knobs.gen.ts
# Go
./bin/knobs gen --lang go --endpoint https://knobs.example.com --api-key <key> --package knobsconfig --out knobs.gen.go
```

The generated file exposes a `Typed(client)` helper that projects the client's
snapshot into the typed `Config` struct/interface.

### Targeting & rollout

`get(key)` returns the base (default) value. Pass an evaluation context to resolve
targeting rules and percentage rollouts client-side:

```ts
knobs.get<number>("maxRetries", { targetingKey: user.id, attributes: { plan: "enterprise" } });
```

```go
v, ok := c.GetFor("maxRetries", knobs.EvalContext{TargetingKey: user.ID, Attributes: map[string]any{"plan": "enterprise"}})
```

Bucketing is deterministic and identical across server and both SDKs (pinned by a
shared golden-vector file), so the same caller always lands in the same variant.

## How it works (one paragraph)

The server stores a typed **schema** and versioned **values** per environment in
Postgres. When values change it writes a new `config_version`, bumps a monotonic
`delivery_revision`, and fires a Postgres `LISTEN/NOTIFY`. A delivery **Hub**
fans that out to every open SSE `/v1/stream` connection, which pushes a fresh
**snapshot** (`{version, revision, schemaHash, values, targeting}`) — or a delta.
SDKs dedupe and reconnect on the monotonic `revision` (never the user-facing
`version`, so a rollback — higher revision, lower version — still applies). See
[`docs/architecture.md`](./docs/architecture.md) for the full end-to-end design.

## Layout

```
cmd/server     — the HTTP server (wiring in internal/app)
cmd/knobs      — the `knobs` CLI (codegen: `knobs gen`)
internal/      — api (handlers/router/authz), store (pgx + migrations), delivery
                 (hub/listener/snapshot/delta), targeting (rule evaluator),
                 schema (typed validation), codegen, authz, auth, config, app
sdk/typescript — the TypeScript SDK (@knobs/sdk)
sdk/go         — the Go SDK (stdlib-only nested module)
web/           — the embedded React admin UI (built into the server binary)
examples/      — copy-pasteable consumer examples
docs/          — architecture / onboarding docs
```

## Development

```sh
make test              # unit tests (no database needed)
make test-integration  # integration tests (Testcontainers — needs Docker)
make build-web         # rebuild the embedded UI bundle
```

The Go SDK is a separate stdlib-only module (`cd sdk/go && go test -race ./...`);
the TS SDK and web UI use vitest (`npm test`). New to the codebase? Start with
[`docs/architecture.md`](./docs/architecture.md).
