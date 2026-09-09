// Package targeting implements the Knobs targeting-rule evaluator: matching
// a request context against a list of rules to resolve a flag/config value,
// including deterministic percentage rollouts.
//
// This package is the canonical reference implementation. The bucketing
// algorithm and condition semantics here are mirrored byte-for-byte by the
// Go and TypeScript SDKs, and testdata/vectors.json is the shared golden
// file all three implementations are pinned against. Any change to the
// evaluation logic must be reflected in that vectors file and in the other
// SDKs.
package targeting

// Operator identifies how a Condition compares an attribute against its
// configured values.
type Operator string

const (
	OpIn       Operator = "in"
	OpNotIn    Operator = "notIn"
	OpEq       Operator = "eq"
	OpNeq      Operator = "neq"
	OpContains Operator = "contains"
	OpGt       Operator = "gt"
	OpGte      Operator = "gte"
	OpLt       Operator = "lt"
	OpLte      Operator = "lte"
)

// Condition tests a single attribute from the evaluation context.
type Condition struct {
	Attribute string   `json:"attribute"`
	Operator  Operator `json:"operator"`
	Values    []any    `json:"values"`
}

// Variant is one weighted outcome of a Rollout.
type Variant struct {
	Value  any     `json:"value"`
	Weight float64 `json:"weight"`
}

// Rollout describes a deterministic percentage split across Variants. Salt
// defaults to the rule's key when empty.
type Rollout struct {
	Salt     string    `json:"salt,omitempty"`
	Variants []Variant `json:"variants"`
}

// Rule is a single ordered targeting rule. Conditions must ALL match for
// the rule to apply (an empty Conditions slice is a catch-all that always
// matches). A matching rule resolves to Value, or - if Rollout is set - to
// the bucketed variant's value.
type Rule struct {
	Conditions []Condition `json:"conditions,omitempty"`
	Value      any         `json:"value,omitempty"`
	Rollout    *Rollout    `json:"rollout,omitempty"`
}

// Map is the set of targeting rules for every key, keyed by key name. Rules
// for a given key are evaluated in order; the first matching rule wins.
type Map map[string][]Rule

// EvalContext carries the request-scoped data rules are evaluated against.
// TargetingKey is the stable identifier (e.g. user ID) rollout bucketing is
// keyed on; Attributes holds arbitrary values conditions match against.
type EvalContext struct {
	TargetingKey string
	Attributes   map[string]any
}
