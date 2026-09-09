// Package schema turns typed field definitions into a JSON Schema validator.
package schema

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

var allowedTypes = map[string]bool{
	"string": true, "int": true, "float": true, "bool": true,
	"json": true, "enum": true, "duration": true,
}

const durationPattern = `^[0-9]+(ns|us|µs|ms|s|m|h)$`

type Field struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
	// Default is parsed and persisted alongside the field but is not
	// emitted into the compiled JSON Schema and is not applied to values
	// in P1 — server-side validation neither enforces nor injects it. It
	// is carried through for P3, where SDK/codegen will emit and apply
	// defaults client-side.
	Default     any      `json:"default,omitempty"`
	Min         *float64 `json:"min,omitempty"`
	Max         *float64 `json:"max,omitempty"`
	Pattern     string   `json:"pattern,omitempty"`
	EnumValues  []any    `json:"enumValues,omitempty"`
	Description string   `json:"description,omitempty"`
}

type Definition struct {
	Fields []Field `json:"fields"`
}

func ValidateDefinition(def Definition) error {
	seen := map[string]bool{}
	for _, f := range def.Fields {
		if f.Name == "" {
			return fmt.Errorf("field name must not be empty")
		}
		if seen[f.Name] {
			return fmt.Errorf("duplicate field name %q", f.Name)
		}
		seen[f.Name] = true
		if !allowedTypes[f.Type] {
			return fmt.Errorf("field %q has unknown type %q", f.Name, f.Type)
		}
		if f.Type == "enum" && len(f.EnumValues) == 0 {
			return fmt.Errorf("enum field %q needs enumValues", f.Name)
		}
		if f.Min != nil && f.Max != nil && *f.Min > *f.Max {
			return fmt.Errorf("field %q has min > max", f.Name)
		}
	}
	return nil
}

// jsonSchemaDoc builds the draft 2020-12 document (as a generic map) for def.
func jsonSchemaDoc(def Definition) map[string]any {
	props := map[string]any{}
	var required []string
	for _, f := range def.Fields {
		props[f.Name] = fieldSchema(f)
		if f.Required {
			required = append(required, f.Name)
		}
	}
	doc := map[string]any{
		"$schema":              "https://json-schema.org/draft/2020-12/schema",
		"type":                 "object",
		"additionalProperties": false,
		"properties":           props,
	}
	if len(required) > 0 {
		doc["required"] = required
	}
	return doc
}

func fieldSchema(f Field) map[string]any {
	switch f.Type {
	case "string":
		s := map[string]any{"type": "string"}
		if f.Pattern != "" {
			s["pattern"] = f.Pattern
		}
		return s
	case "int":
		s := map[string]any{"type": "integer"}
		if f.Min != nil {
			s["minimum"] = *f.Min
		}
		if f.Max != nil {
			s["maximum"] = *f.Max
		}
		return s
	case "float":
		s := map[string]any{"type": "number"}
		if f.Min != nil {
			s["minimum"] = *f.Min
		}
		if f.Max != nil {
			s["maximum"] = *f.Max
		}
		return s
	case "bool":
		return map[string]any{"type": "boolean"}
	case "enum":
		return map[string]any{"enum": f.EnumValues}
	case "duration":
		return map[string]any{"type": "string", "pattern": durationPattern}
	default: // json
		return map[string]any{}
	}
}

// Compile builds a draft 2020-12 JSON Schema document from def and compiles
// it into a validator.
func Compile(def Definition) (*jsonschema.Schema, error) {
	doc := jsonSchemaDoc(def)
	raw, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	loaded, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("knobs://schema", loaded); err != nil {
		return nil, err
	}
	return c.Compile("knobs://schema")
}

// CompileField builds a standalone validator for a single field, wrapping
// f's subschema (as produced by fieldSchema) so a lone value - not just a
// full values map - can be checked against it. Useful for validating
// targeting-rule values, which live outside the config values map but must
// still satisfy the field's type/min/max/pattern/enum constraints.
func CompileField(f Field) (*jsonschema.Schema, error) {
	raw, err := json.Marshal(fieldSchema(f))
	if err != nil {
		return nil, err
	}
	loaded, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	c := jsonschema.NewCompiler()
	res := fmt.Sprintf("knobs://field/%s", f.Name)
	if err := c.AddResource(res, loaded); err != nil {
		return nil, err
	}
	return c.Compile(res)
}

// ValidateFieldValue checks a single value against f's subschema, returning
// a readable error on violation.
func ValidateFieldValue(f Field, value any) error {
	compiled, err := CompileField(f)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return err
	}
	return compiled.Validate(inst)
}

// ValidateValues checks values against a compiled schema, returning a
// readable error on violation.
func ValidateValues(compiled *jsonschema.Schema, values map[string]any) error {
	// Round-trip through JSON so numbers/types match JSON Schema's model.
	raw, err := json.Marshal(values)
	if err != nil {
		return err
	}
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return err
	}
	return compiled.Validate(inst)
}
