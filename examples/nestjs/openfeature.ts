// Reference: register the Knobs OpenFeature provider (optional — use this if
// your app already speaks OpenFeature instead of reading typed config directly).
//
// Requires: @openfeature/server-sdk, @knobs/sdk.

import { OpenFeature } from "@openfeature/server-sdk";
import { createClient, KnobsProvider } from "@knobs/sdk";

export async function registerKnobsOpenFeature(): Promise<void> {
  const client = createClient({
    endpoint: process.env.KNOBS_ENDPOINT!,
    apiKey: process.env.KNOBS_API_KEY!,
    environment: process.env.KNOBS_ENV ?? "prod",
  });

  // setProviderAndWait calls the provider's initialize() → client.ready().
  await OpenFeature.setProviderAndWait(new KnobsProvider(client));
}

// Usage anywhere in the app:
//   const ff = OpenFeature.getClient();
//   const on = await ff.getBooleanValue("featureX", false);
//   const retries = await ff.getNumberValue("maxRetries", 3);
