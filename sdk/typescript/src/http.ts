import type { KnobsOptions, Snapshot } from "./types.js";

/**
 * Fetches the current snapshot from `GET {endpoint}/v1/snapshot`.
 *
 * The environment is implied by the API key, so no `env` query param is sent.
 * `since` is accepted for forward-compat with reconnect-with-since (wired in Task 3's
 * streaming); the core client does not use it.
 *
 * - 404 (no values set for the environment) -> `null`.
 * - Any other non-2xx -> throws an `Error` with the server's `{error}` message.
 */
export async function fetchSnapshot(opts: KnobsOptions, since?: number): Promise<Snapshot | null> {
  // Build the URL by appending to the endpoint rather than via `new URL(path, base)` —
  // the latter resets the path, silently dropping any prefix (e.g. a reverse-proxy mount
  // like "https://gw.example.com/knobs" would become "https://gw.example.com/v1/snapshot").
  const base = opts.endpoint.replace(/\/+$/, "");
  let url = `${base}/v1/snapshot`;
  if (since !== undefined) {
    url += `?since=${encodeURIComponent(String(since))}`;
  }

  const res = await fetch(url, {
    headers: {
      authorization: `Bearer ${opts.apiKey}`,
    },
  });

  if (res.status === 404) {
    return null;
  }

  if (!res.ok) {
    const message = await readErrorMessage(res);
    throw new Error(`knobs: failed to fetch snapshot (${res.status}): ${message}`);
  }

  return (await res.json()) as Snapshot;
}

async function readErrorMessage(res: Response): Promise<string> {
  try {
    const body = (await res.json()) as { error?: string };
    return body.error ?? res.statusText;
  } catch {
    return res.statusText;
  }
}
