// Command knobs is the knobs CLI. Its first subcommand, gen, fetches a
// project's schema from a running knobs server and emits typed accessor
// code for an SDK consumer.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/Rohan-Muslekar/knobs/internal/codegen"
	"github.com/Rohan-Muslekar/knobs/internal/schema"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "knobs: "+err.Error())
		os.Exit(1)
	}
}

// run dispatches to a subcommand, writing generated output to stdout. It's
// factored out of main so it's testable without touching process exit codes
// or the real os.Stdout.
func run(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		printUsage()
		return fmt.Errorf("missing subcommand")
	}

	switch args[0] {
	case "gen":
		return runGen(args[1:], stdout)
	default:
		printUsage()
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `usage: knobs <subcommand> [flags]

Subcommands:
  gen    generate typed accessor code from a project's schema

Run "knobs gen -h" for the gen subcommand's flags.`)
}

// runGen implements `knobs gen`: fetch GET {endpoint}/v1/schema with the
// given Bearer api-key, emit the requested --lang's typed accessor code,
// and write it to --out (or stdout when --out is unset).
func runGen(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("gen", flag.ContinueOnError)
	lang := fs.String("lang", "", `target language for generated code ("ts" or "go")`)
	endpoint := fs.String("endpoint", "", "knobs server base URL, e.g. https://knobs.example.com")
	apiKey := fs.String("api-key", "", "bearer API key scoped to the target environment")
	out := fs.String("out", "", "output file path (default: stdout)")
	pkg := fs.String("package", "knobsconfig", `Go package name for the generated file (--lang go only)`)
	if err := fs.Parse(args); err != nil {
		// -h/--help: flag.ContinueOnError already printed usage to fs.Output() and returns
		// flag.ErrHelp here. That's a clean exit, not a failure — without this check it
		// propagates to main() as an error and `knobs gen -h` exits non-zero with a
		// redundant "knobs: flag: help requested" line after the usage text.
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	if *lang != "ts" && *lang != "go" {
		return fmt.Errorf(`unsupported --lang %q: only "ts" and "go" are supported`, *lang)
	}
	if *endpoint == "" {
		return fmt.Errorf("--endpoint is required")
	}
	if *apiKey == "" {
		return fmt.Errorf("--api-key is required")
	}

	def, schemaHash, err := fetchSchema(*endpoint, *apiKey)
	if err != nil {
		return err
	}

	var src string
	switch *lang {
	case "ts":
		src, err = codegen.EmitTypeScript(def, schemaHash)
		if err != nil {
			return fmt.Errorf("generating typescript: %w", err)
		}
	case "go":
		src, err = codegen.EmitGo(def, schemaHash, *pkg)
		if err != nil {
			return fmt.Errorf("generating go: %w", err)
		}
	}

	if *out == "" {
		_, err := io.WriteString(stdout, src)
		return err
	}
	return os.WriteFile(*out, []byte(src), 0o644)
}

// schemaResponse mirrors the GET /v1/schema payload shape from
// internal/api's handleSchemaDelivery: {definition, schemaVersion, schemaHash}.
// schemaVersion isn't needed by codegen, so it's omitted here.
type schemaResponse struct {
	Definition schema.Definition `json:"definition"`
	SchemaHash string            `json:"schemaHash"`
}

// fetchSchema GETs {endpoint}/v1/schema with a Bearer apiKey and decodes the
// definition + schemaHash the codegen needs.
func fetchSchema(endpoint, apiKey string) (schema.Definition, string, error) {
	url := strings.TrimRight(endpoint, "/") + "/v1/schema"

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return schema.Definition{}, "", fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return schema.Definition{}, "", fmt.Errorf("fetching %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return schema.Definition{}, "", fmt.Errorf("GET %s: unexpected status %d: %s", url, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var out schemaResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return schema.Definition{}, "", fmt.Errorf("decoding schema response from %s: %w", url, err)
	}
	return out.Definition, out.SchemaHash, nil
}
