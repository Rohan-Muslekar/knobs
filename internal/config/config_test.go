package config_test

import (
	"testing"

	"github.com/Rohan-Muslekar/knobs/internal/config"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/knobs")
	t.Setenv("LISTEN_ADDR", "")
	t.Setenv("JWT_SECRET", "s")

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
	t.Setenv("JWT_SECRET", "s")

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

func TestLoadAuthFields(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/knobs")
	t.Setenv("JWT_SECRET", "s3cr3t")
	t.Setenv("ADMIN_EMAIL", "admin@x.com")
	t.Setenv("ADMIN_PASSWORD", "pw")
	t.Setenv("COOKIE_SECURE", "")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.JWTSecret != "s3cr3t" || cfg.AdminEmail != "admin@x.com" || cfg.AdminPassword != "pw" {
		t.Fatalf("auth fields wrong: %+v", cfg)
	}
	if !cfg.CookieSecure {
		t.Fatal("CookieSecure should default true")
	}
}

func TestLoadCookieSecureFalse(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/knobs")
	t.Setenv("JWT_SECRET", "s")
	t.Setenv("COOKIE_SECURE", "false")
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.CookieSecure {
		t.Fatal("CookieSecure should be false when COOKIE_SECURE=false")
	}
}

func TestLoadMissingJWTSecret(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/knobs")
	t.Setenv("JWT_SECRET", "")
	if _, err := config.Load(); err == nil {
		t.Fatal("expected error when JWT_SECRET empty")
	}
}
