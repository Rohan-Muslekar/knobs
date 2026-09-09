# Go example

Knobs has **no official Go SDK in v1** (v1 is TS/JS-only). But the delivery API
is plain HTTP + a Bearer read key, so a Go consumer talks to it directly. This
is a minimal, **stdlib-only** reference client.

- `knobs/client.go` — the client: `Snapshot(ctx)` (fetch current) + `Stream(ctx, since, cb)`
  (SSE live updates, revision-gated), plus typed getters (`Int`/`Bool`/`String`).
- `main.go` — fetch once, then stream live updates with reconnect.

It's a **separate Go module** (`example.com/knobs-consumer`) so it builds on its
own and doesn't touch the main knobs module.

## Run

```bash
KNOBS_ENDPOINT=http://localhost:8080 \
KNOBS_API_KEY=knobs_your_read_key \
  go run .
```

## Build

```bash
go build ./...
```

## Notes

- Dedup on `Revision` (monotonic), never `Version` — a rollback carries a higher
  `Revision` but a lower `Version`, and must still apply.
- JSON numbers decode into `float64`; the `Int` helper converts.
- This reference does snapshot + SSE + reconnect. A production Go client would
  add a slow-poll fallback and richer backoff (mirroring the TS SDK). When a
  first-class Go SDK lands (P4+), prefer that over hand-rolling.
