package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRun_NoSubcommandErrors(t *testing.T) {
	var out bytes.Buffer
	if err := run(nil, &out); err == nil {
		t.Fatal("expected an error when no subcommand is given, got nil")
	}
}

func TestRun_UnknownSubcommandErrors(t *testing.T) {
	var out bytes.Buffer
	if err := run([]string{"bogus"}, &out); err == nil {
		t.Fatal("expected an error for an unknown subcommand, got nil")
	}
}

func TestRunGen_UnknownLangErrors(t *testing.T) {
	var out bytes.Buffer
	err := run([]string{"gen", "--lang", "python", "--endpoint", "http://example.invalid", "--api-key", "k"}, &out)
	if err == nil {
		t.Fatal("expected an error for unsupported --lang, got nil")
	}
}

func TestRunGen_NonTwoXXStatusErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	var out bytes.Buffer
	err := run([]string{"gen", "--lang", "ts", "--endpoint", srv.URL, "--api-key", "k"}, &out)
	if err == nil {
		t.Fatal("expected an error on a non-2xx schema response, got nil")
	}
}

func TestRunGen_WritesTypeScriptToStdout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("Authorization header = %q, want %q", got, "Bearer secret")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"definition":{"fields":[{"name":"enabled","type":"bool","required":true}]},"schemaVersion":1,"schemaHash":"abc123"}`))
	}))
	defer srv.Close()

	var out bytes.Buffer
	if err := run([]string{"gen", "--lang", "ts", "--endpoint", srv.URL, "--api-key", "secret"}, &out); err != nil {
		t.Fatalf("run() returned error: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "export interface AppConfig") {
		t.Fatalf("expected AppConfig interface in stdout output, got:\n%s", got)
	}
	if !strings.Contains(got, `schemaHash = "abc123"`) {
		t.Fatalf("expected schemaHash const in stdout output, got:\n%s", got)
	}
}

func TestRunGen_WritesToOutFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"definition":{"fields":[]},"schemaVersion":1,"schemaHash":"h"}`))
	}))
	defer srv.Close()

	outPath := filepath.Join(t.TempDir(), "config.gen.ts")

	var out bytes.Buffer
	if err := run([]string{"gen", "--lang", "ts", "--endpoint", srv.URL, "--api-key", "k", "--out", outPath}, &out); err != nil {
		t.Fatalf("run() returned error: %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("expected nothing written to stdout when --out is set, got %q", out.String())
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading --out file: %v", err)
	}
	if !strings.Contains(string(data), "AppConfig") {
		t.Fatalf("expected generated TypeScript in --out file, got:\n%s", string(data))
	}
}

func TestRunGen_MissingEndpointErrors(t *testing.T) {
	var out bytes.Buffer
	err := run([]string{"gen", "--lang", "ts", "--api-key", "k"}, &out)
	if err == nil {
		t.Fatal("expected an error when --endpoint is missing, got nil")
	}
}

func TestRunGen_MissingAPIKeyErrors(t *testing.T) {
	var out bytes.Buffer
	err := run([]string{"gen", "--lang", "ts", "--endpoint", "http://example.invalid"}, &out)
	if err == nil {
		t.Fatal("expected an error when --api-key is missing, got nil")
	}
}

func TestRunGen_WritesGoToStdout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("Authorization header = %q, want %q", got, "Bearer secret")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"definition":{"fields":[{"name":"enabled","type":"bool","required":true}]},"schemaVersion":1,"schemaHash":"abc123"}`))
	}))
	defer srv.Close()

	var out bytes.Buffer
	if err := run([]string{"gen", "--lang", "go", "--endpoint", srv.URL, "--api-key", "secret"}, &out); err != nil {
		t.Fatalf("run() returned error: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "type Config struct") {
		t.Fatalf("expected Config struct in stdout output, got:\n%s", got)
	}
	if !strings.Contains(got, `SchemaHash = "abc123"`) {
		t.Fatalf("expected SchemaHash const in stdout output, got:\n%s", got)
	}
	if !strings.Contains(got, "package knobsconfig") {
		t.Fatalf("expected default package name knobsconfig in stdout output, got:\n%s", got)
	}
}

func TestRunGen_GoPackageFlag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"definition":{"fields":[]},"schemaVersion":1,"schemaHash":"h"}`))
	}))
	defer srv.Close()

	var out bytes.Buffer
	if err := run([]string{"gen", "--lang", "go", "--endpoint", srv.URL, "--api-key", "k", "--package", "myconfig"}, &out); err != nil {
		t.Fatalf("run() returned error: %v", err)
	}

	if !strings.Contains(out.String(), "package myconfig") {
		t.Fatalf("expected --package to control the generated package name, got:\n%s", out.String())
	}
}

func TestRunGen_WritesGoToOutFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"definition":{"fields":[]},"schemaVersion":1,"schemaHash":"h"}`))
	}))
	defer srv.Close()

	outPath := filepath.Join(t.TempDir(), "config.gen.go")

	var out bytes.Buffer
	if err := run([]string{"gen", "--lang", "go", "--endpoint", srv.URL, "--api-key", "k", "--out", outPath}, &out); err != nil {
		t.Fatalf("run() returned error: %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("expected nothing written to stdout when --out is set, got %q", out.String())
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading --out file: %v", err)
	}
	if !strings.Contains(string(data), "type Config struct") {
		t.Fatalf("expected generated Go in --out file, got:\n%s", string(data))
	}
}

func TestRunGen_HelpFlagExitsCleanly(t *testing.T) {
	var out bytes.Buffer
	if err := run([]string{"gen", "-h"}, &out); err != nil {
		t.Fatalf("run() with -h should return nil (clean exit), got error: %v", err)
	}
}

func TestRunGen_LongHelpFlagExitsCleanly(t *testing.T) {
	var out bytes.Buffer
	if err := run([]string{"gen", "--help"}, &out); err != nil {
		t.Fatalf("run() with --help should return nil (clean exit), got error: %v", err)
	}
}
