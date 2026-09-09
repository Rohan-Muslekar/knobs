# TypeScript example

Consume `@knobs/sdk` from plain TypeScript.

- `basic.ts` — untyped reads (`get`/`getAll`) + live `onChange`.
- `typed.ts` — compile-checked config via `knobs gen` output (`knobs.gen.ts`).
- `knobs.gen.ts` — a committed **sample** of what `knobs gen --lang ts` emits.

## Setup

```bash
# 1. build the SDK once (produces its dist/ that this example resolves)
cd ../../sdk/typescript && npm install && npm run build && cd -

# 2. install this example (pulls @knobs/sdk via file:../../sdk/typescript)
npm install

# 3. typecheck
npm run typecheck
```

## Run

```bash
KNOBS_ENDPOINT=http://localhost:8080 \
KNOBS_API_KEY=knobs_your_read_key \
KNOBS_ENV=prod \
  node --experimental-strip-types basic.ts
```

## Generate your own typed accessor

```bash
knobs gen --lang ts \
  --endpoint http://localhost:8080 \
  --api-key knobs_your_read_key \
  --out knobs.gen.ts
```

`config.maxRetries` is then a compile-checked `number` instead of a stringly-typed
`get("maxRetries")`.
