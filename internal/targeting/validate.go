package targeting

import (
	"fmt"

	"github.com/Rohan-Muslekar/knobs/internal/schema"
)

var allowedOperators = map[Operator]bool{
	OpIn: true, OpNotIn: true, OpEq: true, OpNeq: true, OpContains: true,
	OpGt: true, OpGte: true, OpLt: true, OpLte: true,
}

// Validate checks m against def: every rule key must be a real schema
// field, each rule must set exactly one of Value/Rollout, static and
// rollout-variant values must satisfy the targeted field's subschema,
// rollout weights must sum to more than zero, and condition operators must
// be known. A condition's operator is not restricted by the targeted
// field's type: conditions test attributes on the caller-supplied
// evaluation context, which has no schema, so an operator's validity can't
// depend on what type the rule happens to be targeting. Condition attribute
// names are likewise not validated - the context is open and may carry
// attributes the schema doesn't know about. (A numeric operator paired with
// a non-numeric context attribute simply never matches at evaluation time -
// see Task 1's evaluator - so it's a harmless no-op, not an error.)
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
		if err := validateCondition(cond); err != nil {
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

func validateCondition(cond Condition) error {
	if !allowedOperators[cond.Operator] {
		return fmt.Errorf("unknown operator %q", cond.Operator)
	}
	return nil
}
