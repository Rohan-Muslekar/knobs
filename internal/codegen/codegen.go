// Package codegen turns a knobs schema.Definition into typed accessor
// source code for a target language. Today that's TypeScript
// (EmitTypeScript, in ts.go); a new language is a new Emit<Lang> function
// alongside it, sharing the field-type mapping conventions defined here.
package codegen

import (
	"fmt"
	"strconv"

	"github.com/Rohan-Muslekar/knobs/internal/schema"
)

// tsType maps a schema field's type to its TypeScript representation, per
// the field-type -> TypeScript mapping from the P3 plan:
//
//	string, duration -> string
//	int, float       -> number
//	bool             -> boolean
//	json             -> unknown
//	enum             -> a string-literal union of its enumValues
//
// An enum field with no enumValues is a schema error, not a codegen
// fallback, so it's reported rather than silently emitted as `never` or
// `unknown`.
func tsType(f schema.Field) (string, error) {
	switch f.Type {
	case "string", "duration":
		return "string", nil
	case "int", "float":
		return "number", nil
	case "bool":
		return "boolean", nil
	case "json":
		return "unknown", nil
	case "enum":
		if len(f.EnumValues) == 0 {
			return "", fmt.Errorf("field %q: enum type requires at least one enumValue", f.Name)
		}
		union := ""
		for i, v := range f.EnumValues {
			if i > 0 {
				union += " | "
			}
			union += enumLiteral(v)
		}
		return union, nil
	default:
		return "", fmt.Errorf("field %q: unsupported field type %q", f.Name, f.Type)
	}
}

// enumLiteral renders a single enumValue as a TypeScript literal type: a
// quoted string for string values (the common case), or the value's default
// Go formatting otherwise (numbers and bools already read as valid TS
// literal types unquoted).
func enumLiteral(v any) string {
	if s, ok := v.(string); ok {
		return strconv.Quote(s)
	}
	return fmt.Sprintf("%v", v)
}
