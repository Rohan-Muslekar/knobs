package codegen

import (
	"fmt"
	"go/format"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/Rohan-Muslekar/knobs/internal/schema"
)

// goIdentifierPattern matches a valid, ASCII-only Go identifier. goFieldName
// only ever produces names built from [A-Za-z0-9] runs (see below), so this
// is really a belt-and-suspenders check rather than something that can fail
// in practice — but it's what makes the "if a name genuinely can't map,
// error" guarantee explicit rather than assumed.
var goIdentifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// nonAlnumRun matches a run of one or more characters that can't appear in
// a bare Go identifier word. It's used to split a field name into words the
// way goFieldName PascalCases them.
var nonAlnumRun = regexp.MustCompile(`[^A-Za-z0-9]+`)

// goFieldName derives an exported, valid Go struct field identifier from a
// schema field name. A field name is untrusted input (it can contain
// spaces, punctuation, or start with a digit — e.g. "max retries", "2fa")
// and has to become a name Go's compiler will accept, so the scheme is:
//
//  1. Split the name on any run of non-alphanumeric characters (spaces,
//     hyphens, underscores, etc. all count as separators) to get "words".
//  2. Upper-case the first rune of each word (a word that starts with a
//     digit, e.g. "2fa" with no separators, is left as-is at this step —
//     you can't upper-case a digit) and concatenate the words back
//     together. A field name that's already a valid Go-style identifier
//     (e.g. "maxRetries") is therefore just its first letter capitalized.
//  3. If the result still starts with a digit (only possible when the
//     very first word started with one, e.g. "2fa" -> "2fa"), prefix it
//     with "Field" so it starts with a letter (e.g. "Field2fa").
//
// The struct field's json tag always carries the real, original field
// name (see jsonTagLiteral), so this derivation only has to produce
// *some* valid, exported identifier — it doesn't need to be reversible.
//
// A name with no alphanumeric characters at all (e.g. "---") can't
// produce any word, and is rejected rather than silently emitted as
// something meaningless.
func goFieldName(name string) (string, error) {
	words := nonAlnumRun.Split(name, -1)

	var b strings.Builder
	for _, w := range words {
		if w == "" {
			continue
		}
		r := []rune(w)
		r[0] = unicode.ToUpper(r[0])
		b.WriteString(string(r))
	}

	result := b.String()
	if result == "" {
		return "", fmt.Errorf("field %q: cannot derive a valid Go identifier from this name", name)
	}
	if unicode.IsDigit([]rune(result)[0]) {
		result = "Field" + result
	}
	if !goIdentifierPattern.MatchString(result) {
		return "", fmt.Errorf("field %q: derived identifier %q is not a valid Go identifier", name, result)
	}
	return result, nil
}

// jsonTagLiteral renders the Go source text of a struct tag carrying
// `json:"<fieldName>"`, where fieldName is the field's real, original
// name (untrusted input) rather than the derived Go identifier — this is
// what keeps JSON round-tripping correct regardless of what goFieldName
// did to the exported field name.
//
// strconv.Quote on fieldName produces a valid, escaped Go string literal
// for the tag *value*, so the json key itself can never break out of its
// quotes. The remaining risk is the outer struct-tag delimiter: Go tags
// are conventionally written with backticks (`json:"x"`), but a field
// name containing a literal backtick would break out of that raw string.
// So: use backticks when the rendered tag content contains none: fall
// back to an interpreted (double-quoted) string literal - re-quoted with
// strconv.Quote, which escapes backslashes and double quotes but leaves
// backticks alone (they aren't special inside "..." strings) - when it
// does. Either way the tag's *value*, once parsed by reflect.StructTag,
// comes back out as exactly fieldName.
func jsonTagLiteral(fieldName string) string {
	tagContent := "json:" + strconv.Quote(fieldName)
	if strings.Contains(tagContent, "`") {
		return strconv.Quote(tagContent)
	}
	return "`" + tagContent + "`"
}

// EmitGo renders def (and the schemaHash it was computed from) as a
// deterministic .gen.go source string:
//
//   - a `Config` struct with one exported field per schema field, typed
//     per the field-type -> Go mapping (see goType), with a
//     `json:"<realFieldName>"` tag and, when present, the field's
//     description and/or (for an enum) its allowed values rendered as a
//     `//` doc comment directly above it;
//   - `const SchemaHash = "<schemaHash>"`;
//   - a typed accessor, `Typed`, that round-trips an SDK client's untyped
//     `GetAll()` through JSON into a Config.
//
// Field order in the struct is exactly def.Fields' order, so the same
// Definition always produces byte-for-byte identical output (like
// EmitTypeScript).
//
// The source is built as plain text and then run through go/format's
// Source, which both gofmt's it (so the emitted file always matches
// what `gofmt -l` expects) and, as a side effect, proves it's
// syntactically valid Go — a bug that produced broken source would fail
// here rather than shipping to a consumer's build. Note that
// format.Source only parses and formats; it does NOT type-check, so it
// can't catch two fields deriving the same Go identifier (a struct with
// a duplicate field name parses fine and only fails at the *consumer's*
// `go build`). That collision is checked explicitly below instead, by
// tracking derived identifiers as they're emitted.
// EmitGo renders a schema definition as a Go source file: a Config struct plus
// a Typed helper that unmarshals a client's snapshot into it.
//
// Field.Required is intentionally not encoded in the generated struct — unlike
// the TS emitter's optional `field?:`. Every Go field is a plain value type, so
// a key missing from the snapshot decodes to the type's zero value. Requiredness
// is a server-side schema/validation concern (a snapshot that passed validation
// already has every required field); the generated Go type is a decode target,
// not a validator, so a pointer-per-optional-field would add nil-checks without
// buying safety.
func EmitGo(def schema.Definition, schemaHash, pkg string) (string, error) {
	var b strings.Builder

	b.WriteString("// Code generated by knobs gen; DO NOT EDIT.\n")
	b.WriteString("// Field order matches the schema definition's field order.\n\n")
	fmt.Fprintf(&b, "package %s\n\n", pkg)
	b.WriteString("import \"encoding/json\"\n\n")
	b.WriteString("type Config struct {\n")

	seen := map[string]string{} // derived Go identifier -> original field name that produced it
	for _, f := range def.Fields {
		fieldType, err := goType(f)
		if err != nil {
			return "", err
		}
		goName, err := goFieldName(f.Name)
		if err != nil {
			return "", err
		}
		if prevName, ok := seen[goName]; ok {
			return "", fmt.Errorf("fields %q and %q both derive the Go identifier %q; rename one in the schema", prevName, f.Name, goName)
		}
		seen[goName] = f.Name

		if f.Description != "" {
			fmt.Fprintf(&b, "// %s\n", sanitizeDescription(f.Description))
		}
		if f.Type == "enum" {
			fmt.Fprintf(&b, "// Allowed values: %s\n", enumLiteralList(f.EnumValues))
		}
		fmt.Fprintf(&b, "%s %s %s\n", goName, fieldType, jsonTagLiteral(f.Name))
	}

	b.WriteString("}\n\n")
	fmt.Fprintf(&b, "const SchemaHash = %q\n\n", schemaHash)
	b.WriteString("func Typed(client interface{ GetAll() map[string]any }) (Config, error) {\n")
	b.WriteString("var cfg Config\n")
	b.WriteString("data, err := json.Marshal(client.GetAll())\n")
	b.WriteString("if err != nil {\n")
	b.WriteString("return cfg, err\n")
	b.WriteString("}\n")
	b.WriteString("if err := json.Unmarshal(data, &cfg); err != nil {\n")
	b.WriteString("return cfg, err\n")
	b.WriteString("}\n")
	b.WriteString("return cfg, nil\n")
	b.WriteString("}\n")

	formatted, err := format.Source([]byte(b.String()))
	if err != nil {
		return "", fmt.Errorf("formatting generated Go source: %w", err)
	}
	return string(formatted), nil
}

// enumLiteralList renders an enum field's allowed values as a
// comma-separated list of Go literals (via enumLiteral), for the
// "Allowed values: ..." doc comment. String values come back
// strconv.Quote'd, so this can never introduce a raw newline or
// unterminated comment even if a value itself contains one.
func enumLiteralList(values []any) string {
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = enumLiteral(v)
	}
	return strings.Join(parts, ", ")
}
