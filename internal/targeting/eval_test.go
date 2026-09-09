package targeting

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// vectorContext mirrors the JSON shape used for an evaluation context in
// testdata/vectors.json. EvalContext itself carries no json tags (its
// shape is pinned by the task interface), so the golden file uses this
// explicit camelCase wrapper and tests convert it by hand.
type vectorContext struct {
	TargetingKey string         `json:"targetingKey"`
	Attributes   map[string]any `json:"attributes,omitempty"`
}

func (c vectorContext) toCtx() EvalContext {
	return EvalContext{TargetingKey: c.TargetingKey, Attributes: c.Attributes}
}

type rolloutCase struct {
	TargetingKey string `json:"targetingKey"`
	Expected     any    `json:"expected"`
}

type rolloutVectors struct {
	Key     string        `json:"key"`
	Rollout Rollout       `json:"rollout"`
	Cases   []rolloutCase `json:"cases"`
}

type resolveVector struct {
	Name     string        `json:"name"`
	Key      string        `json:"key"`
	Base     any           `json:"base"`
	Rules    []Rule        `json:"rules"`
	Context  vectorContext `json:"context"`
	Expected any           `json:"expected"`
}

type resolveAllVector struct {
	Name     string         `json:"name"`
	Base     map[string]any `json:"base"`
	Rules    Map            `json:"rules"`
	Context  vectorContext  `json:"context"`
	Expected map[string]any `json:"expected"`
}

type vectorsFile struct {
	RolloutVectors    rolloutVectors     `json:"rolloutVectors"`
	ResolveVectors    []resolveVector    `json:"resolveVectors"`
	ResolveAllVectors []resolveAllVector `json:"resolveAllVectors"`
}

func loadVectors(t *testing.T) vectorsFile {
	t.Helper()
	data, err := os.ReadFile("testdata/vectors.json")
	if err != nil {
		t.Fatalf("reading testdata/vectors.json: %v", err)
	}
	var vf vectorsFile
	if err := json.Unmarshal(data, &vf); err != nil {
		t.Fatalf("unmarshaling testdata/vectors.json: %v", err)
	}
	return vf
}

// TestBucket_MatchesGoldenVectors is the cross-language rollout-bucketing
// contract: a fixed salt+variants rollout evaluated against a batch of
// targeting keys must land on the exact variant recorded in vectors.json.
// The TS and Go SDKs are pinned to the same file and must agree.
func TestBucket_MatchesGoldenVectors(t *testing.T) {
	vf := loadVectors(t)
	rv := vf.RolloutVectors
	rules := []Rule{{Rollout: &rv.Rollout}}

	if len(rv.Cases) == 0 {
		t.Fatal("rolloutVectors.cases is empty")
	}

	for _, c := range rv.Cases {
		t.Run(c.TargetingKey, func(t *testing.T) {
			got := Resolve(rv.Key, nil, rules, EvalContext{TargetingKey: c.TargetingKey})
			if !reflect.DeepEqual(got, c.Expected) {
				t.Errorf("bucket(%q) = %v, want %v", c.TargetingKey, got, c.Expected)
			}
		})
	}
}

// TestResolve_MatchesGoldenVectors runs every full-evaluation vector
// (conditions + rollout + context -> expected value) recorded in
// vectors.json.
func TestResolve_MatchesGoldenVectors(t *testing.T) {
	vf := loadVectors(t)
	if len(vf.ResolveVectors) == 0 {
		t.Fatal("resolveVectors is empty")
	}

	for _, v := range vf.ResolveVectors {
		t.Run(v.Name, func(t *testing.T) {
			got := Resolve(v.Key, v.Base, v.Rules, v.Context.toCtx())
			if !reflect.DeepEqual(got, v.Expected) {
				t.Errorf("Resolve() = %#v, want %#v", got, v.Expected)
			}
		})
	}
}

// TestResolveAll_MatchesGoldenVectors covers ResolveAll's key-union
// behavior against vectors.json.
func TestResolveAll_MatchesGoldenVectors(t *testing.T) {
	vf := loadVectors(t)
	if len(vf.ResolveAllVectors) == 0 {
		t.Fatal("resolveAllVectors is empty")
	}

	for _, v := range vf.ResolveAllVectors {
		t.Run(v.Name, func(t *testing.T) {
			got := ResolveAll(v.Base, v.Rules, v.Context.toCtx())
			if !reflect.DeepEqual(got, v.Expected) {
				t.Errorf("ResolveAll() = %#v, want %#v", got, v.Expected)
			}
		})
	}
}

// --- Per-operator table tests, independent of the golden file. ---

func TestMatchCondition_Operators(t *testing.T) {
	tests := []struct {
		name  string
		cond  Condition
		attrs map[string]any
		want  bool
	}{
		{
			name:  "in: matches",
			cond:  Condition{Attribute: "country", Operator: OpIn, Values: []any{"IN", "US"}},
			attrs: map[string]any{"country": "US"},
			want:  true,
		},
		{
			name:  "in: no match",
			cond:  Condition{Attribute: "country", Operator: OpIn, Values: []any{"IN", "US"}},
			attrs: map[string]any{"country": "FR"},
			want:  false,
		},
		{
			name:  "in: missing attribute never matches",
			cond:  Condition{Attribute: "country", Operator: OpIn, Values: []any{"IN", "US"}},
			attrs: map[string]any{},
			want:  false,
		},
		{
			name:  "notIn: matches when excluded",
			cond:  Condition{Attribute: "country", Operator: OpNotIn, Values: []any{"IN", "US"}},
			attrs: map[string]any{"country": "FR"},
			want:  true,
		},
		{
			name:  "notIn: no match when included",
			cond:  Condition{Attribute: "country", Operator: OpNotIn, Values: []any{"IN", "US"}},
			attrs: map[string]any{"country": "US"},
			want:  false,
		},
		{
			name:  "notIn: missing attribute matches (documented exception)",
			cond:  Condition{Attribute: "country", Operator: OpNotIn, Values: []any{"IN", "US"}},
			attrs: map[string]any{},
			want:  true,
		},
		{
			name:  "eq: matches",
			cond:  Condition{Attribute: "plan", Operator: OpEq, Values: []any{"pro"}},
			attrs: map[string]any{"plan": "pro"},
			want:  true,
		},
		{
			name:  "eq: no match",
			cond:  Condition{Attribute: "plan", Operator: OpEq, Values: []any{"pro"}},
			attrs: map[string]any{"plan": "free"},
			want:  false,
		},
		{
			name:  "eq: missing attribute never matches",
			cond:  Condition{Attribute: "plan", Operator: OpEq, Values: []any{"pro"}},
			attrs: map[string]any{},
			want:  false,
		},
		{
			name:  "eq: numeric int attribute vs float value",
			cond:  Condition{Attribute: "count", Operator: OpEq, Values: []any{float64(5)}},
			attrs: map[string]any{"count": 5},
			want:  true,
		},
		{
			name:  "neq: matches",
			cond:  Condition{Attribute: "plan", Operator: OpNeq, Values: []any{"pro"}},
			attrs: map[string]any{"plan": "free"},
			want:  true,
		},
		{
			name:  "neq: no match",
			cond:  Condition{Attribute: "plan", Operator: OpNeq, Values: []any{"pro"}},
			attrs: map[string]any{"plan": "pro"},
			want:  false,
		},
		{
			name:  "contains: matches substring",
			cond:  Condition{Attribute: "email", Operator: OpContains, Values: []any{"@acme.com"}},
			attrs: map[string]any{"email": "dev@acme.com"},
			want:  true,
		},
		{
			name:  "contains: no match",
			cond:  Condition{Attribute: "email", Operator: OpContains, Values: []any{"@acme.com"}},
			attrs: map[string]any{"email": "dev@example.com"},
			want:  false,
		},
		{
			name:  "contains: non-string attribute never matches",
			cond:  Condition{Attribute: "age", Operator: OpContains, Values: []any{"1"}},
			attrs: map[string]any{"age": 18},
			want:  false,
		},
		{
			name:  "gt: numeric attribute matches",
			cond:  Condition{Attribute: "age", Operator: OpGt, Values: []any{float64(18)}},
			attrs: map[string]any{"age": float64(21)},
			want:  true,
		},
		{
			name:  "gt: numeric attribute at boundary does not match",
			cond:  Condition{Attribute: "age", Operator: OpGt, Values: []any{float64(18)}},
			attrs: map[string]any{"age": float64(18)},
			want:  false,
		},
		{
			name:  "gte: numeric attribute at boundary matches",
			cond:  Condition{Attribute: "age", Operator: OpGte, Values: []any{float64(18)}},
			attrs: map[string]any{"age": float64(18)},
			want:  true,
		},
		{
			name:  "lt: numeric attribute matches",
			cond:  Condition{Attribute: "age", Operator: OpLt, Values: []any{float64(18)}},
			attrs: map[string]any{"age": float64(12)},
			want:  true,
		},
		{
			name:  "lte: numeric attribute at boundary matches",
			cond:  Condition{Attribute: "age", Operator: OpLte, Values: []any{float64(18)}},
			attrs: map[string]any{"age": float64(18)},
			want:  true,
		},
		{
			name:  "gt: non-numeric attribute never matches",
			cond:  Condition{Attribute: "age", Operator: OpGt, Values: []any{float64(18)}},
			attrs: map[string]any{"age": "not-a-number"},
			want:  false,
		},
		{
			name:  "gt: missing attribute never matches",
			cond:  Condition{Attribute: "age", Operator: OpGt, Values: []any{float64(18)}},
			attrs: map[string]any{},
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchCondition(tt.cond, tt.attrs)
			if got != tt.want {
				t.Errorf("matchCondition(%+v, %v) = %v, want %v", tt.cond, tt.attrs, got, tt.want)
			}
		})
	}
}

func TestMatchConditions_EmptyIsCatchAll(t *testing.T) {
	if !matchConditions(nil, nil) {
		t.Error("nil conditions should always match (catch-all)")
	}
	if !matchConditions([]Condition{}, map[string]any{"anything": "goes"}) {
		t.Error("empty conditions should always match regardless of attributes (catch-all)")
	}
}

func TestBucket_DefaultSaltIsKeyName(t *testing.T) {
	rollout := &Rollout{
		Variants: []Variant{
			{Value: "a", Weight: 1},
			{Value: "b", Weight: 1},
		},
	}
	got := bucket(rollout, "my-flag", "some-user")

	explicit := &Rollout{
		Salt: "my-flag",
		Variants: []Variant{
			{Value: "a", Weight: 1},
			{Value: "b", Weight: 1},
		},
	}
	want := bucket(explicit, "my-flag", "some-user")

	if got != want {
		t.Errorf("bucket with empty salt = %v, want %v (salt defaulting to key name)", got, want)
	}
}

func TestBucket_EmptyTargetingKeyReturnsFirstVariant(t *testing.T) {
	rollout := &Rollout{
		Salt: "s",
		Variants: []Variant{
			{Value: "first", Weight: 1},
			{Value: "second", Weight: 99},
		},
	}
	got := bucket(rollout, "flag", "")
	if got != "first" {
		t.Errorf("bucket with empty targeting key = %v, want %q", got, "first")
	}
}

func TestBucket_IsDeterministic(t *testing.T) {
	rollout := &Rollout{
		Salt: "s",
		Variants: []Variant{
			{Value: "a", Weight: 50},
			{Value: "b", Weight: 50},
		},
	}
	first := bucket(rollout, "flag", "same-user")
	for i := 0; i < 50; i++ {
		if got := bucket(rollout, "flag", "same-user"); got != first {
			t.Fatalf("bucket() is not deterministic: got %v then %v", first, got)
		}
	}
}
