package schema_test

import (
	"testing"

	"github.com/Rohan-Muslekar/knobs/internal/schema"
)

func def(fields ...schema.Field) schema.Definition { return schema.Definition{Fields: fields} }

func TestValidateDefinitionRejectsUnknownType(t *testing.T) {
	err := schema.ValidateDefinition(def(schema.Field{Name: "x", Type: "widget"}))
	if err == nil {
		t.Fatal("expected unknown type rejection")
	}
}

func TestValidateDefinitionRejectsDuplicateNames(t *testing.T) {
	err := schema.ValidateDefinition(def(
		schema.Field{Name: "x", Type: "string"},
		schema.Field{Name: "x", Type: "int"},
	))
	if err == nil {
		t.Fatal("expected duplicate name rejection")
	}
}

func TestCompileAndValidateValues(t *testing.T) {
	f5 := 5.0
	d := def(
		schema.Field{Name: "maxRetries", Type: "int", Required: true, Max: &f5},
		schema.Field{Name: "featureX", Type: "bool"},
	)
	if err := schema.ValidateDefinition(d); err != nil {
		t.Fatalf("valid def rejected: %v", err)
	}
	c, err := schema.Compile(d)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	// valid
	if err := schema.ValidateValues(c, map[string]any{"maxRetries": 3, "featureX": true}); err != nil {
		t.Fatalf("valid values rejected: %v", err)
	}
	// missing required
	if err := schema.ValidateValues(c, map[string]any{"featureX": true}); err == nil {
		t.Fatal("missing required maxRetries should fail")
	}
	// out of range
	if err := schema.ValidateValues(c, map[string]any{"maxRetries": 9}); err == nil {
		t.Fatal("maxRetries=9 exceeds max 5, should fail")
	}
	// unknown field (additionalProperties:false)
	if err := schema.ValidateValues(c, map[string]any{"maxRetries": 1, "bogus": 1}); err == nil {
		t.Fatal("unknown field should fail")
	}
}

func TestCompileEnumField(t *testing.T) {
	d := def(schema.Field{Name: "tier", Type: "enum", EnumValues: []any{"gold", "silver"}})
	c, err := schema.Compile(d)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if err := schema.ValidateValues(c, map[string]any{"tier": "gold"}); err != nil {
		t.Fatalf("member value rejected: %v", err)
	}
	if err := schema.ValidateValues(c, map[string]any{"tier": "bronze"}); err == nil {
		t.Fatal("non-member value should fail")
	}
}

func TestCompileDurationField(t *testing.T) {
	d := def(schema.Field{Name: "ttl", Type: "duration"})
	c, err := schema.Compile(d)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if err := schema.ValidateValues(c, map[string]any{"ttl": "30s"}); err != nil {
		t.Fatalf("30s rejected: %v", err)
	}
	if err := schema.ValidateValues(c, map[string]any{"ttl": "5m"}); err != nil {
		t.Fatalf("5m rejected: %v", err)
	}
	if err := schema.ValidateValues(c, map[string]any{"ttl": "banana"}); err == nil {
		t.Fatal("banana should fail (not a duration)")
	}
	if err := schema.ValidateValues(c, map[string]any{"ttl": "30"}); err == nil {
		t.Fatal("30 (no unit) should fail")
	}
}

func TestCompileStringPatternField(t *testing.T) {
	d := def(schema.Field{Name: "code", Type: "string", Pattern: "^[A-Z]{3}$"})
	c, err := schema.Compile(d)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if err := schema.ValidateValues(c, map[string]any{"code": "ABC"}); err != nil {
		t.Fatalf("ABC rejected: %v", err)
	}
	if err := schema.ValidateValues(c, map[string]any{"code": "ab"}); err == nil {
		t.Fatal("ab should fail pattern")
	}
}

func TestCompileFloatMinMaxField(t *testing.T) {
	zero, one := 0.0, 1.0
	d := def(schema.Field{Name: "ratio", Type: "float", Min: &zero, Max: &one})
	c, err := schema.Compile(d)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if err := schema.ValidateValues(c, map[string]any{"ratio": 0.5}); err != nil {
		t.Fatalf("0.5 rejected: %v", err)
	}
	if err := schema.ValidateValues(c, map[string]any{"ratio": 1.5}); err == nil {
		t.Fatal("1.5 exceeds max 1.0, should fail")
	}
	if err := schema.ValidateValues(c, map[string]any{"ratio": "x"}); err == nil {
		t.Fatal("wrong type (string) should fail")
	}
}
