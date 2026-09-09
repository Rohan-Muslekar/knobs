# Go example

> **Superseded for production use.** Knobs now ships a real Go SDK at
> [`sdk/go`](../../sdk/go) — `go get github.com/Rohan-Muslekar/knobs/sdk/go`, then
> `New`/`Ready`/`Get`/`GetAll`/`OnChange`/`Close`, plus `knobs gen --lang go` for a
> typed `Config`. Use that instead of hand-rolling a client. This example is kept
> as a "how it works under the hood" illustration of the delivery API's wire
> contract (snapshot + SSE, revision-gated, Bearer-authed) — the same contract
> `sdk/go` implements, minus the poll fallback and richer reconnect backoff.

The delivery API is plain HTTP + a Bearer read key, so a Go consumer can talk to
it directly without any SDK. This is a minimal, **stdlib-only** reference client
that does exactly that.

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
- This reference does snapshot + SSE + reconnect, but skips the slow-poll
  fallback and richer backoff that `sdk/go` adds — use `sdk/go` for anything
  beyond learning how the wire contract works.
