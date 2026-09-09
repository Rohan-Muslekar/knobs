package targeting

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"reflect"
	"strings"
)

// Resolve evaluates rules in order against ctx and returns the value of the
// first matching rule (bucketed through its Rollout when one is set), or
// base if no rule matches.
func Resolve(key string, base any, rules []Rule, ctx EvalContext) any {
	for _, rule := range rules {
		if !matchConditions(rule.Conditions, ctx.Attributes) {
			continue
		}
		if rule.Rollout != nil {
			return bucket(rule.Rollout, key, ctx.TargetingKey)
		}
		return rule.Value
	}
	return base
}

// ResolveAll evaluates every key present in either base or rules and
// returns the resolved value for each.
func ResolveAll(base map[string]any, rules Map, ctx EvalContext) map[string]any {
	keys := make(map[string]struct{}, len(base)+len(rules))
	for k := range base {
		keys[k] = struct{}{}
	}
	for k := range rules {
		keys[k] = struct{}{}
	}

	result := make(map[string]any, len(keys))
	for k := range keys {
		result[k] = Resolve(k, base[k], rules[k], ctx)
	}
	return result
}

// bucket deterministically assigns targetingKey to one of rollout's
// variants. salt defaults to key when unset; an empty targetingKey always
// resolves to the first variant.
func bucket(rollout *Rollout, key string, targetingKey string) any {
	if len(rollout.Variants) == 0 {
		return nil
	}
	if targetingKey == "" {
		return rollout.Variants[0].Value
	}

	salt := rollout.Salt
	if salt == "" {
		salt = key
	}

	sum := sha256.Sum256([]byte(salt + ":" + key + ":" + targetingKey))
	n := binary.BigEndian.Uint32(sum[:4])
	frac := float64(n) / 4294967296.0

	var total float64
	for _, v := range rollout.Variants {
		total += v.Weight
	}
	point := frac * total

	var cumulative float64
	for _, v := range rollout.Variants {
		cumulative += v.Weight
		if point < cumulative {
			return v.Value
		}
	}
	// Floating-point rounding can leave point == cumulative for the last
	// variant; fall back to it rather than dropping the assignment.
	return rollout.Variants[len(rollout.Variants)-1].Value
}

// matchConditions reports whether every condition matches attrs. An empty
// slice is a catch-all that always matches.
func matchConditions(conditions []Condition, attrs map[string]any) bool {
	for _, c := range conditions {
		if !matchCondition(c, attrs) {
			return false
		}
	}
	return true
}

func matchCondition(c Condition, attrs map[string]any) bool {
	val, present := attrs[c.Attribute]
	if !present {
		// A missing attribute never matches, except notIn: absence is
		// vacuously "not in" any set of values.
		return c.Operator == OpNotIn
	}

	switch c.Operator {
	case OpIn:
		return containsValue(c.Values, val)
	case OpNotIn:
		return !containsValue(c.Values, val)
	case OpEq:
		if len(c.Values) == 0 {
			return false
		}
		return equalValues(val, c.Values[0])
	case OpNeq:
		if len(c.Values) == 0 {
			return false
		}
		return !equalValues(val, c.Values[0])
	case OpContains:
		if len(c.Values) == 0 {
			return false
		}
		s, ok := val.(string)
		if !ok {
			return false
		}
		sub, ok := c.Values[0].(string)
		if !ok {
			return false
		}
		return strings.Contains(s, sub)
	case OpGt, OpGte, OpLt, OpLte:
		if len(c.Values) == 0 {
			return false
		}
		vf, ok := toFloat64(val)
		if !ok {
			return false
		}
		tf, ok := toFloat64(c.Values[0])
		if !ok {
			return false
		}
		switch c.Operator {
		case OpGt:
			return vf > tf
		case OpGte:
			return vf >= tf
		case OpLt:
			return vf < tf
		case OpLte:
			return vf <= tf
		}
	}
	return false
}

func containsValue(values []any, needle any) bool {
	for _, v := range values {
		if equalValues(v, needle) {
			return true
		}
	}
	return false
}

// equalValues compares two condition/attribute values. Numeric values are
// normalized to float64 first so JSON's single number type (and any Go
// numeric literal) compares consistently regardless of int vs float
// representation; everything else falls back to a panic-safe deep compare.
func equalValues(a, b any) bool {
	if af, ok := toFloat64(a); ok {
		bf, ok := toFloat64(b)
		if !ok {
			return false
		}
		return af == bf
	}
	return reflect.DeepEqual(a, b)
}

// toFloat64 normalizes any numeric type - including json.Number and every
// Go integer/float kind - to float64. Non-numeric values report ok=false.
func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int8:
		return float64(n), true
	case int16:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint8:
		return float64(n), true
	case uint16:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}
