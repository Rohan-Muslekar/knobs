package targeting

import (
	"fmt"

	"github.com/Rohan-Muslekar/knobs/internal/schema"
)

// numericTypes are the field types eligible for the ordering operators
// (gt/gte/lt/lte). duration values are lexically stored as strings but are
// numeric in intent, so they're allowed here too.
var numericTypes = map[string]bool{
	"int": true, "float": true, "duration": true,
}

var allowedOperators = map[Operator]bool{
	OpIn: true, OpNotIn: true, OpEq: true, OpNeq: true, OpContains: true,
	OpGt: true, OpGte: true, OpLt: true, OpLte: true,
}

// Validate checks m against def: every rule key must be a real schema
// field, each rule must set exactly one of Value/Rollout, static and
// rollout-variant values must satisfy the targeted field's subschema,
// rollout weights must sum to more than zero, and condition operators must
// be known (with the ordering operators restricted to numeric-ish fields).
// Condition attribute names are not validated - the evaluation context is
// open and may carry attributes the schema doesn't know about.
//
// A nil or empty m is valid. Errors are prefixed with the offending path,
// e.g. targeting["maxRetries"].rules[0].variants[1].value: ...
func Validate(def schema.Definition, m Map) error {
	if len(m) == 0 {
		return nil
	}

	fields := make(map[string]schema.Field, len(def.Fields))
	for _, f := range def.Fields {
		fields[f.Name] = f
	}

	for key, rules := range m {
		f, ok := fields[key]
		if !ok {
			return fmt.Errorf("targeting[%q]: no such field in schema", key)
		}
		for i, rule := range rules {
			path := fmt.Sprintf("targeting[%q].rules[%d]", key, i)
			if err := validateRule(path, f, rule); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateRule(path string, f schema.Field, rule Rule) error {
	hasValue := rule.Value != nil
	hasRollout := rule.Rollout != nil

	if hasValue && hasRollout {
		return fmt.Errorf("%s: exactly one of value/rollout must be set, got both", path)
	}
	if !hasValue && !hasRollout {
		return fmt.Errorf("%s: exactly one of value/rollout must be set, got neither", path)
	}

	for i, cond := range rule.Conditions {
		if err := validateCondition(f, cond); err != nil {
			return fmt.Errorf("%s.conditions[%d]: %w", path, i, err)
		}
	}

	if hasValue {
		if err := schema.ValidateFieldValue(f, rule.Value); err != nil {
			return fmt.Errorf("%s.value: %w", path, err)
		}
		return nil
	}

	return validateRollout(path, f, rule.Rollout)
}

func validateRollout(path string, f schema.Field, rollout *Rollout) error {
	var totalWeight float64
	for i, v := range rollout.Variants {
		if err := schema.ValidateFieldValue(f, v.Value); err != nil {
			return fmt.Errorf("%s.variants[%d].value: %w", path, i, err)
		}
		totalWeight += v.Weight
	}
	if totalWeight <= 0 {
		return fmt.Errorf("%s.rollout: variant weights must sum to more than zero, got %v", path, totalWeight)
	}
	return nil
}

func validateCondition(f schema.Field, cond Condition) error {
	if !allowedOperators[cond.Operator] {
		return fmt.Errorf("unknown operator %q", cond.Operator)
	}
	switch cond.Operator {
	case OpGt, OpGte, OpLt, OpLte:
		if !numericTypes[f.Type] {
			return fmt.Errorf("operator %q is only valid on numeric fields, field %q is %q", cond.Operator, f.Name, f.Type)
		}
	}
	return nil
}
