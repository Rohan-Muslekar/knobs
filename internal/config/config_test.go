package config_test

import (
	"testing"

	"github.com/Rohan-Muslekar/knobs/internal/config"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/knobs")
	t.Setenv("LISTEN_ADDR", "")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ListenAddr != ":8080" {
		t.Fatalf("ListenAddr = %q, want :8080", cfg.ListenAddr)
	}
	if cfg.DatabaseURL != "postgres://localhost/knobs" {
		t.Fatalf("DatabaseURL = %q", cfg.DatabaseURL)
	}
}

func TestLoadOverrideAddr(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/knobs")
	t.Setenv("LISTEN_ADDR", ":9000")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ListenAddr != ":9000" {
		t.Fatalf("ListenAddr = %q, want :9000", cfg.ListenAddr)
	}
}

func TestLoadMissingDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")

	if _, err := config.Load(); err == nil {
		t.Fatal("expected error when DATABASE_URL is empty")
	}
}
