# knobs (Go SDK)

Go SDK for [Knobs](../../) — live, in-memory config reads over the delivery API. Fetches the
initial snapshot, keeps it live via SSE with poll fallback, and exposes zero-network `Get`/`GetAll` reads.

A separate Go module (`sdk/go/go.mod`), stdlib-only — importing it doesn't pull in the server's dependencies.

## Install

```bash
go get github.com/Rohan-Muslekar/knobs/sdk/go
```

## Usage

```go
import "github.com/Rohan-Muslekar/knobs/sdk/go"

client := knobs.New(knobs.Options{
	Endpoint:    "https://knobs.example.com",
	APIKey:      os.Getenv("KNOBS_API_KEY"),
	Environment: "prod",
})

ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()
if err := client.Ready(ctx); err != nil {
	log.Fatal(err)
}

client.Get("maxRetries") // single key, no network call
client.GetAll()          // full resolved config, no network call

unsubscribe := client.OnChange(func(values map[string]any) {
	// fires whenever a newer snapshot is applied (stream, reconnect, or poll
	// fallback) — NOT on the initial load, since Ready/GetAll already
	// deliver that baseline.
	log.Println("config updated", values)
})

// ... later, e.g. on process shutdown
unsubscribe()
client.Close() // stops streaming/reconnect/poll — safe to call more than once
```

### `knobs.Options`

| Field                | Required | Description                                                                 |
| --------------------- | -------- | ----------------------------------------------------------------------------- |
| `Endpoint`            | yes      | Base URL of the Knobs delivery API. A path prefix (e.g. a reverse-proxy mount) is preserved. |
| `APIKey`              | yes      | Sent as `Authorization: Bearer <APIKey>`. The environment is implied by this key. |
| `Environment`         | no       | Informational; the server resolves the actual environment from the API key. |
| `PollInterval`        | no       | Slow-poll fallback interval, belt-and-suspenders alongside the SSE stream. Zero/negative defaults to 60s. |
| `ExpectedSchemaHash`  | no       | The `schemaHash` your generated code was built against. A mismatch logs a warning via `Logger` (not an error) — see codegen below. |
| `HTTPClient`          | no       | Defaults to `http.DefaultClient`. |
| `Logger`              | no       | `*slog.Logger`; defaults to `slog.Default()`. |
| `DisableDeltas`       | no       | Opts out of incremental delta frames on the stream (they're requested by default — the zero value keeps them on). See "Delta frames" below. |

### `Client`

- `Ready(ctx context.Context) error` — fetches the first snapshot and, once loaded, starts the background stream + poll goroutines. A 404 (no values set for the environment yet) is not an error — the client just starts out empty.
- `Get(key string) (any, bool)` — reads one key from the in-memory snapshot. No network call.
- `GetAll() map[string]any` — reads a copy of the full in-memory snapshot. No network call; mutating the returned map never affects the client's internal state.
- `OnChange(cb func(values map[string]any)) (unsubscribe func())` — registers a listener fired on every later snapshot swap; returns an unsubscribe function, safe to call more than once.
- `Close()` — stops streaming, reconnect, and polling. Idempotent, and safe even if `Ready` was never called.

Updates are gated by the snapshot's `Revision`, not `Version`: a snapshot only replaces the current one if its
revision is strictly higher, so a stale replay is ignored and a rollback (higher revision, lower version) still
applies. Always compare/dedupe on `Revision` — never `Version` — if you're touching the internals.

### Delta frames

By default the client requests `?deltas=1` on the stream, so most updates arrive as a small
set/remove diff instead of the full config. This is transparent to callers — `Get`/`GetAll`/`OnChange`
behave the same either way — and it still works unmodified against a server that only ever sends full
snapshots. Set `Options.DisableDeltas: true` to always request full snapshot frames instead.

## Codegen: typed config with `knobs gen`

Rather than reading untyped `GetAll()` values by string key, generate a typed accessor from your project's
schema with the `knobs` CLI:

```bash
knobs gen --lang go --package knobsconfig --endpoint https://knobs.example.com --api-key <api-key> --out config.gen.go
```

This fetches your project's schema (`GET {endpoint}/v1/schema`, authenticated with `--api-key`) and emits a
`config.gen.go` (package `knobsconfig`, or whatever `--package` names) with:

- a `Config` struct — one exported field per schema field, typed per the field's schema type, `json:"<name>"` tagged with the real field name;
- `const SchemaHash` — pass this as `ExpectedSchemaHash` so a drifted server schema is flagged;
- `func Typed(client interface{ GetAll() map[string]any }) (Config, error)` — round-trips a `knobs.Client`'s `GetAll()` through JSON into a `Config`.

Regenerate whenever the schema changes; `config.gen.go` is generated output ("DO NOT EDIT" is stamped at the top) —
commit it like any other generated artifact, or regenerate it in CI.

```go
import (
	"github.com/Rohan-Muslekar/knobs/sdk/go"
	"your/module/path/knobsconfig" // wherever you wrote --out
)

client := knobs.New(knobs.Options{
	Endpoint:           "https://knobs.example.com",
	APIKey:             os.Getenv("KNOBS_API_KEY"),
	Environment:        "prod",
	ExpectedSchemaHash: knobsconfig.SchemaHash,
})
if err := client.Ready(ctx); err != nil {
	log.Fatal(err)
}

cfg, err := knobsconfig.Typed(client) // Config — compile-checked field access
if err != nil {
	log.Fatal(err)
}
cfg.MaxRetries // int64, not `any`
```

## Running a server to point this at

You need a running Knobs server and a read-scoped API key to try any of the above against something real.
The short version: `make build && ./bin/server` (with `DATABASE_URL`, `JWT_SECRET`, `ADMIN_EMAIL`,
`ADMIN_PASSWORD` set), then in the admin UI create a project, define a schema, create an environment, save
values, and create a **read API key** for that environment (shown once — copy it). See
[`examples/README.md`](../../examples/README.md#getting-an-endpoint--api-key) for the full walkthrough.
Point `Endpoint` at that server and `APIKey` at that key.

## Development

```bash
go test ./... -race   # incl. the e2e lifecycle test (fake server, no real network)
go vet ./...
gofmt -l .
```
