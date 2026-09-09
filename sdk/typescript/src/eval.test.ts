import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import { bucket, resolve, resolveAll } from "./eval.js";
import type { EvalContext, Rollout, Rule } from "./types.js";

// The golden-vector contract: this file is a byte-for-byte copy of
// internal/targeting/testdata/vectors.json (Task 5 brief). The TS evaluator + bucketing must
// match every vector recorded there — that's how the TS and Go SDKs stay pinned together.
const vectorsPath = fileURLToPath(new URL("./__testdata__/vectors.json", import.meta.url));

interface RolloutCase {
  targetingKey: string;
  expected: unknown;
}

interface RolloutVectors {
  key: string;
  rollout: Rollout;
  cases: RolloutCase[];
}

interface ResolveVector {
  name: string;
  key: string;
  base: unknown;
  rules: Rule[] | null;
  context: EvalContext;
  expected: unknown;
}

interface ResolveAllVector {
  name: string;
  base: Record<string, unknown>;
  rules: Record<string, Rule[]>;
  context: EvalContext;
  expected: Record<string, unknown>;
}

interface VectorsFile {
  rolloutVectors: RolloutVectors[];
  resolveVectors: ResolveVector[];
  resolveAllVectors: ResolveAllVector[];
}

const vectors: VectorsFile = JSON.parse(readFileSync(vectorsPath, "utf8"));

describe("bucket (golden vectors)", () => {
  it("has at least one rollout vector set", () => {
    expect(vectors.rolloutVectors.length).toBeGreaterThan(0);
  });

  for (const { key, rollout, cases } of vectors.rolloutVectors) {
    describe(key, () => {
      it("has at least one case", () => {
        expect(cases.length).toBeGreaterThan(0);
      });

      for (const c of cases) {
        it(`buckets ${JSON.stringify(c.targetingKey)} to ${JSON.stringify(c.expected)}`, () => {
          expect(bucket(rollout, key, c.targetingKey)).toEqual(c.expected);
        });
      }
    });
  }
});

describe("resolve (golden vectors)", () => {
  it("has at least one vector", () => {
    expect(vectors.resolveVectors.length).toBeGreaterThan(0);
  });

  for (const v of vectors.resolveVectors) {
    it(v.name, () => {
      expect(resolve(v.key, v.base, v.rules, v.context)).toEqual(v.expected);
    });
  }
});

describe("resolveAll (golden vectors)", () => {
  it("has at least one vector", () => {
    expect(vectors.resolveAllVectors.length).toBeGreaterThan(0);
  });

  for (const v of vectors.resolveAllVectors) {
    it(v.name, () => {
      expect(resolveAll(v.base, v.rules, v.context)).toEqual(v.expected);
    });
  }
});
