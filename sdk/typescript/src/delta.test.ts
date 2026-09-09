import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import { applyDelta, canApplyDelta } from "./delta.js";
import type { Delta, Snapshot } from "./types.js";

describe("canApplyDelta", () => {
  it("is true when from matches the current revision", () => {
    const current: Snapshot = { version: 1, revision: 5, schemaHash: "h", values: {} };
    const delta: Delta = { type: "delta", version: 1, revision: 6, from: 5, schemaHash: "h" };

    expect(canApplyDelta(current, delta)).toBe(true);
  });

  it("is false on a from-mismatch (defensive path — caller should full-resync instead)", () => {
    const current: Snapshot = { version: 1, revision: 5, schemaHash: "h", values: {} };
    const delta: Delta = { type: "delta", version: 1, revision: 9, from: 7, schemaHash: "h" };

    expect(canApplyDelta(current, delta)).toBe(false);
  });
});

describe("applyDelta", () => {
  it("merges values.set/remove onto a copy of current.values", () => {
    const current: Snapshot = {
      version: 1,
      revision: 1,
      schemaHash: "h1",
      values: { retries: 3, timeout: 30 },
    };
    const delta: Delta = {
      type: "delta",
      version: 1,
      revision: 2,
      from: 1,
      schemaHash: "h1",
      values: { set: { retries: 5 }, remove: ["timeout"] },
    };

    const result = applyDelta(current, delta);

    expect(result).toEqual({
      version: 1,
      revision: 2,
      schemaHash: "h1",
      values: { retries: 5 },
      targeting: {},
    });
  });

  it("merges targeting.set/remove onto a copy of current.targeting", () => {
    const current: Snapshot = {
      version: 1,
      revision: 1,
      schemaHash: "h1",
      values: {},
      targeting: { beta: [{ value: false }], gamma: [{ value: true }] },
    };
    const delta: Delta = {
      type: "delta",
      version: 1,
      revision: 2,
      from: 1,
      schemaHash: "h1",
      targeting: { set: { beta: [{ value: true }] }, remove: ["gamma"] },
    };

    const result = applyDelta(current, delta);

    expect(result.targeting).toEqual({ beta: [{ value: true }] });
  });

  it("treats an absent values/targeting on the delta as no change on that side", () => {
    const current: Snapshot = {
      version: 1,
      revision: 1,
      schemaHash: "h1",
      values: { a: 1 },
      targeting: { beta: [{ value: false }] },
    };
    const delta: Delta = { type: "delta", version: 2, revision: 2, from: 1, schemaHash: "h2" };

    const result = applyDelta(current, delta);

    expect(result).toEqual({
      version: 2,
      revision: 2,
      schemaHash: "h2",
      values: { a: 1 },
      targeting: { beta: [{ value: false }] },
    });
  });

  it("treats an absent set/remove within values/targeting as no change on that side", () => {
    const current: Snapshot = {
      version: 1,
      revision: 1,
      schemaHash: "h1",
      values: { a: 1 },
      targeting: { beta: [{ value: false }] },
    };
    // values has no `remove`, targeting has no `set` — both should be no-ops on their side.
    const delta: Delta = {
      type: "delta",
      version: 1,
      revision: 2,
      from: 1,
      schemaHash: "h1",
      values: { set: { b: 2 } },
      targeting: { remove: ["beta"] },
    };

    const result = applyDelta(current, delta);

    expect(result.values).toEqual({ a: 1, b: 2 });
    expect(result.targeting).toEqual({});
  });

  it("does not mutate the input snapshot or its nested objects", () => {
    const current: Snapshot = {
      version: 1,
      revision: 1,
      schemaHash: "h1",
      values: { a: 1 },
      targeting: { beta: [{ value: false }] },
    };
    const snapshotBefore = JSON.parse(JSON.stringify(current));
    const delta: Delta = {
      type: "delta",
      version: 1,
      revision: 2,
      from: 1,
      schemaHash: "h1",
      values: { set: { a: 2, c: 3 }, remove: ["a"] },
      targeting: { set: { gamma: [{ value: true }] }, remove: ["beta"] },
    };

    applyDelta(current, delta);

    expect(current).toEqual(snapshotBefore);
  });
});

// The golden-vector contract: this file is a byte-for-byte copy of
// internal/delivery/testdata/delta-vectors.json (Task 1's brief). The TS SDK's applyDelta
// must reproduce every step recorded there — that's how the TS and Go SDKs stay pinned
// together on delta apply semantics.
//
// The vectors file carries a `_comment` key documenting its own shape (see the file itself)
// — plain JSON.parse ignores it, so no strict/`additionalProperties: false` validation is
// used here.
const vectorsPath = fileURLToPath(new URL("./__testdata__/delta-vectors.json", import.meta.url));

interface DeltaVectorsFile {
  base: Snapshot;
  steps: Array<{ delta: Delta; expected: Snapshot }>;
}

const vectors: DeltaVectorsFile = JSON.parse(readFileSync(vectorsPath, "utf8"));

describe("applyDelta (golden vectors)", () => {
  it("has at least one step", () => {
    expect(vectors.steps.length).toBeGreaterThan(0);
  });

  it("replays every step from base, matching the expected snapshot at each step", () => {
    let current = vectors.base;
    for (const step of vectors.steps) {
      expect(canApplyDelta(current, step.delta)).toBe(true);
      current = applyDelta(current, step.delta);
      expect(current).toEqual(step.expected);
    }
  });
});
