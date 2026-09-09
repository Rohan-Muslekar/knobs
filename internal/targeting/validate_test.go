package targeting_test

import (
	"strings"
	"testing"

	"github.com/Rohan-Muslekar/knobs/internal/schema"
	"github.com/Rohan-Muslekar/knobs/internal/targeting"
)

func testDef() schema.Definition {
	f5 := 5.0
	f0 := 0.0
	return schema.Definition{Fields: []schema.Field{
		{Name: "maxRetries", Type: "int", Min: &f0, Max: &f5},
		{Name: "featureX", Type: "bool"},
		{Name: "tier", Type: "enum", EnumValues: []any{"gold", "silver"}},
		{Name: "label", Type: "string", Pattern: "^[a-z]+$"},
		{Name: "ttl", Type: "duration"},
	}}
}

func TestValidateNilOrEmptyMap(t *testing.T) {
	if err := targeting.Validate(testDef(), nil); err != nil {
		t.Fatalf("nil map should be valid, got %v", err)
	}
	if err := targeting.Validate(testDef(), targeting.Map{}); err != nil {
		t.Fatalf("empty map should be valid, got %v", err)
	}
}

func TestValidateFullyValidMap(t *testing.T) {
	m := targeting.Map{
		"maxRetries": {
			{
				Conditions: []targeting.Condition{{Attribute: "plan", Operator: targeting.OpEq, Values: []any{"pro"}}},
				Value:      3,
			},
			{
				Rollout: &targeting.Rollout{
					Salt: "s1",
					Variants: []targeting.Variant{
						{Value: 1, Weight: 1},
						{Value: 2, Weight: 1},
					},
				},
			},
		},
		"featureX": {
			{Value: true},
		},
	}
	if err := targeting.Validate(testDef(), m); err != nil {
		t.Fatalf("valid map rejected: %v", err)
	}
}

func TestValidateUnknownKey(t *testing.T) {
	m := targeting.Map{
		"doesNotExist": {{Value: 1}},
	}
	err := targeting.Validate(testDef(), m)
	if err == nil {
		t.Fatal("expected error for unknown key")
	}
	if !strings.Contains(err.Error(), "doesNotExist") {
		t.Fatalf("error should mention the unknown key, got %v", err)
	}
}

func TestValidateValueWrongType(t *testing.T) {
	m := targeting.Map{
		"maxRetries": {{Value: "not-an-int"}},
	}
	err := targeting.Validate(testDef(), m)
	if err == nil {
		t.Fatal("expected error for wrong-type value")
	}
	if !strings.Contains(err.Error(), `targeting["maxRetries"].rules[0].value`) {
		t.Fatalf("error should be path-prefixed, got %v", err)
	}
}

func TestValidateValueOutOfRange(t *testing.T) {
	m := targeting.Map{
		"maxRetries": {{Value: 9}},
	}
	if err := targeting.Validate(testDef(), m); err == nil {
		t.Fatal("expected error for out-of-range value")
	}
}

func TestValidateVariantOutOfRange(t *testing.T) {
	m := targeting.Map{
		"maxRetries": {
			{
				Rollout: &targeting.Rollout{
					Variants: []targeting.Variant{
						{Value: 1, Weight: 1},
						{Value: 99, Weight: 1},
					},
				},
			},
		},
	}
	err := targeting.Validate(testDef(), m)
	if err == nil {
		t.Fatal("expected error for out-of-range variant value")
	}
	if !strings.Contains(err.Error(), `targeting["maxRetries"].rules[0].variants[1].value`) {
		t.Fatalf("error should be path-prefixed to the offending variant, got %v", err)
	}
}

func TestValidateVariantWrongEnum(t *testing.T) {
	m := targeting.Map{
		"tier": {
			{
				Rollout: &targeting.Rollout{
					Variants: []targeting.Variant{
						{Value: "gold", Weight: 1},
						{Value: "bronze", Weight: 1},
					},
				},
			},
		},
	}
	if err := targeting.Validate(testDef(), m); err == nil {
		t.Fatal("expected error for non-member enum variant value")
	}
}

func TestValidateBothValueAndRolloutSet(t *testing.T) {
	m := targeting.Map{
		"featureX": {
			{
				Value:   true,
				Rollout: &targeting.Rollout{Variants: []targeting.Variant{{Value: true, Weight: 1}}},
			},
		},
	}
	err := targeting.Validate(testDef(), m)
	if err == nil {
		t.Fatal("expected error when both value and rollout are set")
	}
	if !strings.Contains(err.Error(), "both") {
		t.Fatalf("error should mention both value/rollout set, got %v", err)
	}
}

func TestValidateNeitherValueNorRolloutSet(t *testing.T) {
	m := targeting.Map{
		"featureX": {{}},
	}
	err := targeting.Validate(testDef(), m)
	if err == nil {
		t.Fatal("expected error when neither value nor rollout is set")
	}
	if !strings.Contains(err.Error(), "neither") {
		t.Fatalf("error should mention neither value/rollout set, got %v", err)
	}
}

func TestValidateRolloutWeightsSumToZero(t *testing.T) {
	m := targeting.Map{
		"featureX": {
			{
				Rollout: &targeting.Rollout{
					Variants: []targeting.Variant{
						{Value: true, Weight: 0},
						{Value: false, Weight: 0},
					},
				},
			},
		},
	}
	err := targeting.Validate(testDef(), m)
	if err == nil {
		t.Fatal("expected error when rollout weights sum to zero")
	}
	if !strings.Contains(err.Error(), "weight") {
		t.Fatalf("error should mention weights, got %v", err)
	}
}

func TestValidateRolloutNegativeVariantWeight(t *testing.T) {
	m := targeting.Map{
		"maxRetries": {
			{
				Rollout: &targeting.Rollout{
					Variants: []targeting.Variant{
						{Value: 1, Weight: -10},
						{Value: 2, Weight: 20},
					},
				},
			},
		},
	}
	err := targeting.Validate(testDef(), m)
	if err == nil {
		t.Fatal("expected error for negative variant weight even though total weight is positive")
	}
	if !strings.Contains(err.Error(), `targeting["maxRetries"].rules[0].rollout.variants[0].weight`) {
		t.Fatalf("error should be path-prefixed to the offending variant's weight, got %v", err)
	}
	if !strings.Contains(err.Error(), "negative") {
		t.Fatalf("error should mention the weight is negative, got %v", err)
	}
}

func TestValidateRolloutEmptyVariants(t *testing.T) {
	m := targeting.Map{
		"featureX": {
			{Rollout: &targeting.Rollout{}},
		},
	}
	if err := targeting.Validate(testDef(), m); err == nil {
		t.Fatal("expected error for rollout with no variants (weights sum to zero)")
	}
}

func TestValidateNumericOperatorOnStringFieldIsAllowed(t *testing.T) {
	// A condition's operator tests an open, caller-supplied context
	// attribute - it has no relationship to the type of the field the rule
	// is targeting. Targeting the string-valued "label" field with a
	// gt-on-"age" condition is a legitimate rule (e.g. "adults see this
	// label"), so it must be accepted.
	m := targeting.Map{
		"label": {
			{
				Conditions: []targeting.Condition{{Attribute: "age", Operator: targeting.OpGt, Values: []any{18}}},
				Value:      "abc",
			},
		},
	}
	if err := targeting.Validate(testDef(), m); err != nil {
		t.Fatalf("numeric operator condition on a string-typed rule field should be allowed, got %v", err)
	}
}

func TestValidateNumericOperatorOnNumericFieldOK(t *testing.T) {
	m := targeting.Map{
		"maxRetries": {
			{
				Conditions: []targeting.Condition{{Attribute: "score", Operator: targeting.OpGte, Values: []any{5}}},
				Value:      2,
			},
		},
	}
	if err := targeting.Validate(testDef(), m); err != nil {
		t.Fatalf("numeric operator on numeric-typed field should be fine, got %v", err)
	}
}

func TestValidateUnknownOperator(t *testing.T) {
	m := targeting.Map{
		"featureX": {
			{
				Conditions: []targeting.Condition{{Attribute: "x", Operator: targeting.Operator("bogus"), Values: []any{1}}},
				Value:      true,
			},
		},
	}
	if err := targeting.Validate(testDef(), m); err == nil {
		t.Fatal("expected error for unknown operator")
	}
}

func TestValidateDurationValueValid(t *testing.T) {
	m := targeting.Map{
		"ttl": {{Value: "30s"}},
	}
	if err := targeting.Validate(testDef(), m); err != nil {
		t.Fatalf("valid duration value rejected: %v", err)
	}
}

func TestValidateDurationValueInvalid(t *testing.T) {
	m := targeting.Map{
		"ttl": {{Value: "nope"}},
	}
	err := targeting.Validate(testDef(), m)
	if err == nil {
		t.Fatal("expected error for malformed duration value")
	}
	if !strings.Contains(err.Error(), `targeting["ttl"].rules[0].value`) {
		t.Fatalf("error should be path-prefixed, got %v", err)
	}
}

func TestValidateStringPatternViolation(t *testing.T) {
	m := targeting.Map{
		"label": {{Value: "NOT-LOWERCASE"}},
	}
	err := targeting.Validate(testDef(), m)
	if err == nil {
		t.Fatal("expected error for value violating the field's pattern")
	}
	if !strings.Contains(err.Error(), `targeting["label"].rules[0].value`) {
		t.Fatalf("error should be path-prefixed, got %v", err)
	}
}

func TestValidateUnknownConditionAttributeIsAllowed(t *testing.T) {
	// Context is open: condition attribute names are never checked against
	// the schema, only the rule's own value/variant values are.
	m := targeting.Map{
		"featureX": {
			{
				Conditions: []targeting.Condition{{Attribute: "totallyUnknownAttr", Operator: targeting.OpEq, Values: []any{"x"}}},
				Value:      true,
			},
		},
	}
	if err := targeting.Validate(testDef(), m); err != nil {
		t.Fatalf("unknown condition attribute should be allowed, got %v", err)
	}
}
