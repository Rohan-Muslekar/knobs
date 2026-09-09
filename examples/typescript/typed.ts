// Typed TypeScript usage — compile-checked config via `knobs gen` output.
//
// `knobs.gen.ts` here is a committed SAMPLE of the codegen output. Regenerate
// it for your own project's schema with:
//   knobs gen --lang ts --endpoint <url> --api-key <key> --out knobs.gen.ts

import { createClient } from "@knobs/sdk";
import { typed, schemaHash, type AppConfig } from "./knobs.gen.js";

const knobs = createClient({
  endpoint: process.env.KNOBS_ENDPOINT ?? "http://localhost:8080",
  apiKey: process.env.KNOBS_API_KEY ?? "",
  environment: process.env.KNOBS_ENV ?? "prod",
  // Pass the generated hash so the SDK warns if the running schema has drifted
  // from the types you generated (regenerate when that happens).
  expectedSchemaHash: schemaHash,
});

await knobs.ready();

// `config` is typed as AppConfig — `config.maxRetries` is a compile-checked
// number, not a stringly-typed lookup.
const config: AppConfig = typed(knobs);
console.log("maxRetries (number):", config.maxRetries);
console.log("tier ('gold' | 'silver'):", config.tier);

if (config.featureX) {
  console.log("feature X is on");
}

// A helper that always reflects the latest snapshot (typed() reads live state).
function currentConfig(): AppConfig {
  return typed(knobs);
}

knobs.onChange(() => {
  console.log("reloaded maxRetries:", currentConfig().maxRetries);
});

await new Promise((r) => setTimeout(r, 30_000));
knobs.close();
