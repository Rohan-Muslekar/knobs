import { createHash } from "node:crypto";
import type { Condition, EvalContext, Rollout, Rule } from "./types.js";

/**
 * Evaluates `rules` in order against `ctx` and returns the value of the first matching rule
 * (bucketed through its rollout when one is set), or `base` if no rule matches.
 *
 * This mirrors `internal/targeting.Resolve` byte-for-byte — see testdata/vectors.json, the
 * shared golden file both implementations are pinned against.
 */
export function resolve(key: string, base: unknown, rules: Rule[] | undefined | null, ctx: EvalContext): unknown {
  for (const rule of rules ?? []) {
    if (!matchConditions(rule.conditions, ctx.attributes)) continue;
    if (rule.rollout) return bucket(rule.rollout, key, ctx.targetingKey);
    return rule.value;
  }
  return base;
}

/** Evaluates every key present in either `base` or `rules` and returns the resolved value for each. */
export function resolveAll(
  base: Record<string, unknown>,
  rules: Record<string, Rule[]> | undefined | null,
  ctx: EvalContext,
): Record<string, unknown> {
  const keys = new Set<string>([...Object.keys(base), ...Object.keys(rules ?? {})]);

  const result: Record<string, unknown> = {};
  for (const k of keys) {
    result[k] = resolve(k, base[k], (rules ?? {})[k], ctx);
  }
  return result;
}

/**
 * Deterministically assigns `targetingKey` to one of `rollout`'s variants. `salt` defaults to
 * `key` when unset; an empty `targetingKey` always resolves to the first variant.
 */
export function bucket(rollout: Rollout, key: string, targetingKey: string): unknown {
  if (rollout.variants.length === 0) return undefined;
  if (targetingKey === "") return rollout.variants[0]!.value;

  const salt = rollout.salt || key;

  const digest = createHash("sha256").update(`${salt}:${key}:${targetingKey}`).digest();
  const n = digest.readUInt32BE(0);
  const frac = n / 4294967296;

  let total = 0;
  for (const v of rollout.variants) total += v.weight;
  const point = frac * total;

  let cumulative = 0;
  for (const v of rollout.variants) {
    cumulative += v.weight;
    if (point < cumulative) return v.value;
  }
  // Floating-point rounding can leave point == cumulative for the last variant; fall back to
  // it rather than dropping the assignment.
  return rollout.variants[rollout.variants.length - 1]!.value;
}

/** Reports whether every condition matches `attrs`. An empty/absent array is a catch-all that always matches. */
function matchConditions(conditions: Condition[] | undefined, attrs: Record<string, unknown> | undefined): boolean {
  for (const c of conditions ?? []) {
    if (!matchCondition(c, attrs)) return false;
  }
  return true;
}

function matchCondition(c: Condition, attrs: Record<string, unknown> | undefined): boolean {
  const present = attrs !== undefined && Object.prototype.hasOwnProperty.call(attrs, c.attribute);
  const val = present ? attrs![c.attribute] : undefined;

  if (!present) {
    // A missing attribute never matches, except notIn: absence is vacuously "not in" any set
    // of values.
    return c.operator === "notIn";
  }

  switch (c.operator) {
    case "in":
      return containsValue(c.values, val);
    case "notIn":
      return !containsValue(c.values, val);
    case "eq":
      if (c.values.length === 0) return false;
      return equalValues(val, c.values[0]);
    case "neq":
      if (c.values.length === 0) return false;
      return !equalValues(val, c.values[0]);
    case "contains": {
      if (c.values.length === 0) return false;
      if (typeof val !== "string") return false;
      const sub = c.values[0];
      if (typeof sub !== "string") return false;
      return val.includes(sub);
    }
    case "gt":
    case "gte":
    case "lt":
    case "lte": {
      if (c.values.length === 0) return false;
      const vf = toFinite(val);
      if (vf === undefined) return false;
      const tf = toFinite(c.values[0]);
      if (tf === undefined) return false;
      switch (c.operator) {
        case "gt":
          return vf > tf;
        case "gte":
          return vf >= tf;
        case "lt":
          return vf < tf;
        case "lte":
          return vf <= tf;
      }
    }
  }
  return false;
}

function containsValue(values: unknown[], needle: unknown): boolean {
  return values.some((v) => equalValues(v, needle));
}

/**
 * Compares two condition/attribute values. Numeric values are compared as numbers so JS's
 * single number type compares consistently; everything else falls back to a deep equality
 * check.
 */
function equalValues(a: unknown, b: unknown): boolean {
  const af = toFinite(a);
  if (af !== undefined) {
    const bf = toFinite(b);
    if (bf === undefined) return false;
    return af === bf;
  }
  return deepEqual(a, b);
}

/** Coerces `v` to a finite number. Non-numeric/non-finite values report `undefined`. */
function toFinite(v: unknown): number | undefined {
  if (typeof v === "number") return Number.isFinite(v) ? v : undefined;
  return undefined;
}

function deepEqual(a: unknown, b: unknown): boolean {
  if (Object.is(a, b)) return true;
  if (typeof a !== typeof b) return false;
  if (a === null || b === null) return a === b;
  if (Array.isArray(a) || Array.isArray(b)) {
    if (!Array.isArray(a) || !Array.isArray(b)) return false;
    if (a.length !== b.length) return false;
    return a.every((v, i) => deepEqual(v, b[i]));
  }
  if (typeof a === "object" && typeof b === "object") {
    const aKeys = Object.keys(a as Record<string, unknown>);
    const bKeys = Object.keys(b as Record<string, unknown>);
    if (aKeys.length !== bKeys.length) return false;
    return aKeys.every((k) =>
      Object.prototype.hasOwnProperty.call(b, k) &&
      deepEqual((a as Record<string, unknown>)[k], (b as Record<string, unknown>)[k]),
    );
  }
  return false;
}
