import type { Delta, Rule, Snapshot } from "./types.js";

/**
 * Whether `d` chains cleanly onto `current` — i.e. `d.from` matches the revision `current`
 * is already at. `applyDelta` assumes this has already been checked; a caller that gets
 * `false` back should not call `applyDelta` and instead fall back to a full resync (refetch
 * a full snapshot and apply that).
 */
export function canApplyDelta(current: Snapshot, d: Delta): boolean {
  return d.from === current.revision;
}

/**
 * Applies a Delta on top of `current`, producing the resulting Snapshot. Pure — does not
 * mutate `current` (or any of its nested objects); the caller is responsible for checking
 * `canApplyDelta(current, d)` first (this function doesn't re-check `from`).
 *
 * `values.set`/`values.remove` and `targeting.set`/`targeting.remove` (and `values`/
 * `targeting` themselves) may all be absent — treated as "no change" on that side, same as
 * the wire contract's "absent means empty."
 */
export function applyDelta(current: Snapshot, d: Delta): Snapshot {
  const values = { ...current.values, ...d.values?.set };
  for (const key of d.values?.remove ?? []) {
    delete values[key];
  }

  const targeting: Record<string, Rule[]> = { ...current.targeting, ...d.targeting?.set };
  for (const key of d.targeting?.remove ?? []) {
    delete targeting[key];
  }

  return {
    version: d.version,
    revision: d.revision,
    schemaHash: d.schemaHash,
    values,
    targeting,
  };
}
