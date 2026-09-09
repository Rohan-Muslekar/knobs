# NestJS example

Reference snippets (drop into a Nest app; `npm install @knobs/sdk`).

- `knobs.module.ts` — a `@Global()` `KnobsModule` exporting a `KnobsService` that
  owns one `KnobsClient`: loads the first snapshot in `onModuleInit`, closes the
  stream in `onApplicationShutdown`, and exposes typed `config()` + `onChange()`.
- `payments.service.ts` — a feature service reading typed, live config via DI.
- `openfeature.ts` — optional: register the `KnobsProvider` with OpenFeature instead.

## Wire it up

```ts
// app.module.ts
import { Module } from "@nestjs/common";
import { KnobsModule } from "./knobs.module.js";
import { PaymentsService } from "./payments.service.js";

@Module({
  imports: [KnobsModule],           // global — KnobsService injectable anywhere
  providers: [PaymentsService],
})
export class AppModule {}
```

Enable shutdown hooks in `main.ts` so `onApplicationShutdown` fires:

```ts
const app = await NestFactory.create(AppModule);
app.enableShutdownHooks();
```

## Env

```
KNOBS_ENDPOINT=http://localhost:8080
KNOBS_API_KEY=knobs_your_read_key
KNOBS_ENV=prod
```

## Types

Generate `knobs.gen.ts` (imported by `knobs.module.ts`) for your schema:

```bash
knobs gen --lang ts --endpoint $KNOBS_ENDPOINT --api-key $KNOBS_API_KEY --out knobs.gen.ts
```

## Prod PEH note

PEH is EU-residency + self-hosted, so serving prod PEH means running a Knobs
instance inside the eu-west-2 estate and pointing `KNOBS_ENDPOINT` at it.
