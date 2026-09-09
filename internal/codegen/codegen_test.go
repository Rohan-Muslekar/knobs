package codegen

import (
	"os"
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
