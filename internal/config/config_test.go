package config_test

import (
	"testing"

	"github.com/Rohan-Muslekar/knobs/internal/config"
)

// testJWTSecret satisfies config.Load's >=32 char floor so tests that don't
// care about the secret's value itself don't have to restate the length
// requirement inline.
const testJWTSecret = "01234567890123456789012345678901"

func TestLoadDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/knobs")
	t.Setenv("LISTEN_ADDR", "")
	t.Setenv("JWT_SECRET", testJWTSecret)

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
	t.Setenv("JWT_SECRET", testJWTSecret)

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
	t.Setenv("JWT_SECRET", testJWTSecret)
	t.Setenv("ADMIN_EMAIL", "admin@x.com")
	t.Setenv("ADMIN_PASSWORD", "pw")
	t.Setenv("COOKIE_SECURE", "")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.JWTSecret != testJWTSecret || cfg.AdminEmail != "admin@x.com" || cfg.AdminPassword != "pw" {
		t.Fatalf("auth fields wrong: %+v", cfg)
	}
	if !cfg.CookieSecure {
		t.Fatal("CookieSecure should default true")
	}
}

func TestLoadCookieSecureFalse(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/knobs")
	t.Setenv("JWT_SECRET", testJWTSecret)
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

func TestLoadRejectsShortJWTSecret(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/knobs")
	t.Setenv("JWT_SECRET", "short-secret")
	if _, err := config.Load(); err == nil {
		t.Fatal("expected error when JWT_SECRET is under 32 characters")
	}
}

func TestLoadTrustProxy(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{"unset defaults false", "", false},
		{"anything but the exact string true is false", "1", false},
		{"true enables it", "true", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("DATABASE_URL", "postgres://localhost/knobs")
			t.Setenv("JWT_SECRET", testJWTSecret)
			t.Setenv("TRUST_PROXY", tc.raw)

			cfg, err := config.Load()
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if cfg.TrustProxy != tc.want {
				t.Fatalf("TrustProxy = %v, want %v", cfg.TrustProxy, tc.want)
			}
		})
	}
}
