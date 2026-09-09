package codegen

import (
	"go/format"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/Rohan-Muslekar/knobs/internal/schema"
)

// fixtureDefinition covers every field type the emitter has to handle: one
// field of each schema.Field type, a non-required field (nickname), and a
// field with a description (name) — matching the golden file in
// testdata/expected.gen.ts.
func fixtureDefinition() schema.Definition {
	return schema.Definition{
		Fields: []schema.Field{
			{Name: "name", Type: "string", Required: true, Description: "Human-readable app name"},
			{Name: "nickname", Type: "string", Required: false},
			{Name: "maxRetries", Type: "int", Required: true},
			{Name: "threshold", Type: "float", Required: true},
			{Name: "enabled", Type: "bool", Required: true},
			{Name: "metadata", Type: "json", Required: true},
			{Name: "timeout", Type: "duration", Required: true},
			{Name: "tier", Type: "enum", Required: true, EnumValues: []any{"gold", "silver"}},
		},
	}
}

func TestEmitTypeScript_MatchesGolden(t *testing.T) {
	got, err := EmitTypeScript(fixtureDefinition(), "testhash123")
	if err != nil {
		t.Fatalf("EmitTypeScript returned error: %v", err)
	}

	want, err := os.ReadFile("testdata/expected.gen.ts")
	if err != nil {
		t.Fatalf("reading golden file: %v", err)
	}

	if got != string(want) {
		t.Fatalf("EmitTypeScript output does not match golden byte-for-byte.\n--- got ---\n%s\n--- want ---\n%s", got, string(want))
	}
}

func TestEmitTypeScript_EnumWithoutValuesErrors(t *testing.T) {
	def := schema.Definition{
		Fields: []schema.Field{
			{Name: "tier", Type: "enum", Required: true},
		},
	}

	_, err := EmitTypeScript(def, "hash")
	if err == nil {
		t.Fatal("expected an error for an enum field with no enumValues, got nil")
	}
}

func TestEmitTypeScript_NonRequiredFieldIsOptional(t *testing.T) {
	def := schema.Definition{
		Fields: []schema.Field{
			{Name: "nickname", Type: "string", Required: false},
		},
	}

	got, err := EmitTypeScript(def, "hash")
	if err != nil {
		t.Fatalf("EmitTypeScript returned error: %v", err)
	}
	if !strings.Contains(got, "nickname?: string;") {
		t.Fatalf("expected optional field syntax `nickname?: string;` in output, got:\n%s", got)
	}
}

func TestEmitTypeScript_NonIdentifierFieldNameIsQuoted(t *testing.T) {
	def := schema.Definition{
		Fields: []schema.Field{
			{Name: "max-retries", Type: "int", Required: true},
		},
	}

	got, err := EmitTypeScript(def, "hash")
	if err != nil {
		t.Fatalf("EmitTypeScript returned error: %v", err)
	}
	if !strings.Contains(got, `"max-retries": number;`) {
		t.Fatalf("expected a quoted property key for a non-identifier field name, got:\n%s", got)
	}
}

func TestEmitTypeScript_IdentifierFieldNameIsBare(t *testing.T) {
	def := schema.Definition{
		Fields: []schema.Field{
			{Name: "maxRetries", Type: "int", Required: true},
		},
	}

	got, err := EmitTypeScript(def, "hash")
	if err != nil {
		t.Fatalf("EmitTypeScript returned error: %v", err)
	}
	if !strings.Contains(got, "  maxRetries: number;") {
		t.Fatalf("expected a bare property key for a valid identifier field name, got:\n%s", got)
	}
	if strings.Contains(got, `"maxRetries"`) {
		t.Fatalf("valid identifier field name should not be quoted, got:\n%s", got)
	}
}

func TestEmitTypeScript_DescriptionCommentInjectionIsNeutralized(t *testing.T) {
	def := schema.Definition{
		Fields: []schema.Field{
			{
				Name:        "evil",
				Type:        "string",
				Required:    true,
				Description: "ok */ export const x = doEvil(); /**",
			},
		},
	}

	got, err := EmitTypeScript(def, "hash")
	if err != nil {
		t.Fatalf("EmitTypeScript returned error: %v", err)
	}

	// The malicious payload must never appear as a real top-level statement:
	// it should only ever show up as inert text inside the doc comment.
	if strings.Contains(got, "\nexport const x = doEvil();") {
		t.Fatalf("description was not neutralized: it broke out of the doc comment and injected a top-level statement:\n%s", got)
	}

	// Find the comment line and check it doesn't contain a raw "*/" other
	// than the one that legitimately closes it at the end of the line.
	var commentLine string
	for _, line := range strings.Split(got, "\n") {
		if strings.Contains(line, "doEvil") {
			commentLine = line
			break
		}
	}
	if commentLine == "" {
		t.Fatalf("expected the description to appear in the output, got:\n%s", got)
	}
	inner := strings.TrimSuffix(strings.TrimSpace(commentLine), "*/")
	if strings.Contains(inner, "*/") {
		t.Fatalf("description contains an unneutralized comment terminator, comment closed early: %q", commentLine)
	}
}

func TestEmitTypeScript_MultilineDescriptionCollapsedToOneLine(t *testing.T) {
	def := schema.Definition{
		Fields: []schema.Field{
			{Name: "x", Type: "string", Required: true, Description: "line one\nline two"},
		},
	}

	got, err := EmitTypeScript(def, "hash")
	if err != nil {
		t.Fatalf("EmitTypeScript returned error: %v", err)
	}
	if !strings.Contains(got, "/** line one line two */") {
		t.Fatalf("expected a multi-line description to collapse onto a single comment line, got:\n%s", got)
	}
}

func TestEmitTypeScript_RequiredFieldIsNotOptional(t *testing.T) {
	def := schema.Definition{
		Fields: []schema.Field{
			{Name: "enabled", Type: "bool", Required: true},
		},
	}

	got, err := EmitTypeScript(def, "hash")
	if err != nil {
		t.Fatalf("EmitTypeScript returned error: %v", err)
	}
	if !strings.Contains(got, "enabled: boolean;") {
		t.Fatalf("expected required field syntax `enabled: boolean;` in output, got:\n%s", got)
	}
	if strings.Contains(got, "enabled?:") {
		t.Fatalf("required field must not be marked optional, got:\n%s", got)
	}
}

// --- EmitGo ---

func TestEmitGo_MatchesGolden(t *testing.T) {
	got, err := EmitGo(fixtureDefinition(), "testhash123", "knobsconfig")
	if err != nil {
		t.Fatalf("EmitGo returned error: %v", err)
	}

	want, err := os.ReadFile("testdata/expected.gen.go")
	if err != nil {
		t.Fatalf("reading golden file: %v", err)
	}

	if got != string(want) {
		t.Fatalf("EmitGo output does not match golden byte-for-byte.\n--- got ---\n%s\n--- want ---\n%s", got, string(want))
	}
}

func TestEmitGo_OutputIsValidGo(t *testing.T) {
	got, err := EmitGo(fixtureDefinition(), "testhash", "knobsconfig")
	if err != nil {
		t.Fatalf("EmitGo returned error: %v", err)
	}

	if _, err := format.Source([]byte(got)); err != nil {
		t.Fatalf("EmitGo output is not valid Go: %v\n--- output ---\n%s", err, got)
	}
}

func TestEmitGo_FieldTypeMapping(t *testing.T) {
	got, err := EmitGo(fixtureDefinition(), "testhash", "knobsconfig")
	if err != nil {
		t.Fatalf("EmitGo returned error: %v", err)
	}

	cases := map[string]string{
		"MaxRetries": "int64",   // int -> int64
		"Threshold":  "float64", // float -> float64
		"Enabled":    "bool",    // bool -> bool
		"Name":       "string",  // string -> string
		"Timeout":    "string",  // duration -> string
		"Tier":       "string",  // enum -> string
		"Metadata":   "any",     // json -> any
	}
	for field, wantType := range cases {
		re := regexp.MustCompile(`(?m)^\s*` + field + `\s+` + wantType + `\s`)
		if !re.MatchString(got) {
			t.Fatalf("expected field %q typed %q in output, got:\n%s", field, wantType, got)
		}
	}
}

func TestEmitGo_JSONTagsCarryRealFieldNames(t *testing.T) {
	got, err := EmitGo(fixtureDefinition(), "testhash", "knobsconfig")
	if err != nil {
		t.Fatalf("EmitGo returned error: %v", err)
	}

	for _, name := range []string{"name", "nickname", "maxRetries", "threshold", "enabled", "metadata", "timeout", "tier"} {
		tag := `json:"` + name + `"`
		if !strings.Contains(got, tag) {
			t.Fatalf("expected json tag %q in output, got:\n%s", tag, got)
		}
	}
}

func TestEmitGo_SchemaHashConstAndTypedFunc(t *testing.T) {
	got, err := EmitGo(fixtureDefinition(), "testhash123", "knobsconfig")
	if err != nil {
		t.Fatalf("EmitGo returned error: %v", err)
	}

	if !strings.Contains(got, `SchemaHash = "testhash123"`) {
		t.Fatalf("expected SchemaHash const in output, got:\n%s", got)
	}
	if !strings.Contains(got, "func Typed(client interface{ GetAll() map[string]any }) (Config, error)") {
		t.Fatalf("expected Typed accessor in output, got:\n%s", got)
	}
}

func TestEmitGo_EnumWithoutValuesErrors(t *testing.T) {
	def := schema.Definition{
		Fields: []schema.Field{
			{Name: "tier", Type: "enum", Required: true},
		},
	}

	_, err := EmitGo(def, "hash", "knobsconfig")
	if err == nil {
		t.Fatal("expected an error for an enum field with no enumValues, got nil")
	}
}

func TestEmitGo_NonIdentifierFieldNameBecomesValidPascalCase(t *testing.T) {
	def := schema.Definition{
		Fields: []schema.Field{
			{Name: "max-retries", Type: "int", Required: true},
		},
	}

	got, err := EmitGo(def, "hash", "knobsconfig")
	if err != nil {
		t.Fatalf("EmitGo returned error: %v", err)
	}
	if !strings.Contains(got, "MaxRetries") {
		t.Fatalf("expected derived identifier MaxRetries in output, got:\n%s", got)
	}
	if !strings.Contains(got, `json:"max-retries"`) {
		t.Fatalf(`expected json:"max-retries" tag preserving the real name, got:\n%s`, got)
	}

	if _, err := format.Source([]byte(got)); err != nil {
		t.Fatalf("output with a non-identifier field name is not valid Go: %v\n%s", err, got)
	}
}

func TestEmitGo_LeadingDigitFieldNameGetsPrefixed(t *testing.T) {
	def := schema.Definition{
		Fields: []schema.Field{
			{Name: "2fa", Type: "bool", Required: true},
		},
	}

	got, err := EmitGo(def, "hash", "knobsconfig")
	if err != nil {
		t.Fatalf("EmitGo returned error: %v", err)
	}
	if !strings.Contains(got, "Field2fa") {
		t.Fatalf("expected derived identifier Field2fa in output, got:\n%s", got)
	}
	if !strings.Contains(got, `json:"2fa"`) {
		t.Fatalf(`expected json:"2fa" tag preserving the real name, got:\n%s`, got)
	}
}

func TestEmitGo_NameWithNoAlphanumericsErrors(t *testing.T) {
	def := schema.Definition{
		Fields: []schema.Field{
			{Name: "---", Type: "string", Required: true},
		},
	}

	_, err := EmitGo(def, "hash", "knobsconfig")
	if err == nil {
		t.Fatal("expected an error for a field name with no alphanumeric characters, got nil")
	}
}

func TestEmitGo_CollidingDerivedIdentifiersErrors(t *testing.T) {
	// "max-retries" and "maxRetries" are distinct, schema-valid field
	// names (schema.ValidateDefinition only forbids exact duplicates),
	// but both PascalCase to the same Go identifier, MaxRetries. That
	// collision can't be caught by go/format.Source (it only parses and
	// formats — a struct with a duplicate field name is syntactically
	// fine and would only fail at the consumer's own `go build`), so
	// EmitGo has to catch it itself before emitting.
	def := schema.Definition{
		Fields: []schema.Field{
			{Name: "max-retries", Type: "int", Required: true},
			{Name: "maxRetries", Type: "int", Required: true},
		},
	}

	_, err := EmitGo(def, "hash", "knobsconfig")
	if err == nil {
		t.Fatal("expected an error when two field names derive the same Go identifier, got nil")
	}
	if !strings.Contains(err.Error(), "max-retries") || !strings.Contains(err.Error(), "maxRetries") {
		t.Fatalf("expected the error to name both colliding fields, got: %v", err)
	}
}

func TestEmitGo_DescriptionCommentInjectionIsNeutralized(t *testing.T) {
	def := schema.Definition{
		Fields: []schema.Field{
			{
				Name:        "evil",
				Type:        "string",
				Required:    true,
				Description: "ok\nfunc Backdoor() {}",
			},
		},
	}

	got, err := EmitGo(def, "hash", "knobsconfig")
	if err != nil {
		t.Fatalf("EmitGo returned error: %v", err)
	}
	if strings.Contains(got, "\nfunc Backdoor() {}") {
		t.Fatalf("description was not neutralized: it broke out of the doc comment as a top-level declaration:\n%s", got)
	}
	if _, err := format.Source([]byte(got)); err != nil {
		t.Fatalf("output is not valid Go: %v\n%s", err, got)
	}
}
