// Mirrors the targeting shape the server validates and evaluates. Kept as
// plain structural types (not tied to any particular field's value type)
// since a rule's `value` / rollout variant `value` is whatever the target
// field's schema type allows.

export type Operator = "in" | "notIn" | "eq" | "neq" | "contains" | "gt" | "gte" | "lt" | "lte";

export type Condition = {
  attribute: string;
  operator: Operator;
  values: unknown[];
};

export type RolloutVariant = {
  value: unknown;
  weight: number;
};

export type Rollout = {
  salt?: string;
  variants: RolloutVariant[];
};

// Exactly one of `value` / `rollout` should be set; the server enforces this.
export type Rule = {
  conditions?: Condition[];
  value?: unknown;
  rollout?: Rollout;
};
