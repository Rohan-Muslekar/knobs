// Package config loads server settings from the environment.
package config

import (
	"errors"
	"os"
)

// minJWTSecretLen is the floor for JWT_SECRET: short secrets are brute-
// forceable and undermine the whole point of signing sessions.
const minJWTSecretLen = 32

// Config holds server runtime settings. Knobs never bootstraps its own config
// from itself; these come from the process environment.
type Config struct {
	DatabaseURL   string
	ListenAddr    string
	JWTSecret     string
	AdminEmail    string
	AdminPassword string
	CookieSecure  bool

	// TrustProxy tells the HTTP layer whether to honor X-Forwarded-For for
	// client-IP-keyed logic (e.g. the login rate limiter). Defaults to
	// false: that header is client-settable, so trusting it without a
	// known, correctly-configured reverse proxy in front of Knobs would let
	// an attacker forge a fresh IP per request and bypass any such limit.
	TrustProxy bool
}

// Load reads settings from environment variables and applies defaults.
func Load() (Config, error) {
	cfg := Config{
		DatabaseURL:   os.Getenv("DATABASE_URL"),
		ListenAddr:    os.Getenv("LISTEN_ADDR"),
		JWTSecret:     os.Getenv("JWT_SECRET"),
		AdminEmail:    os.Getenv("ADMIN_EMAIL"),
		AdminPassword: os.Getenv("ADMIN_PASSWORD"),
		CookieSecure:  os.Getenv("COOKIE_SECURE") != "false",
		TrustProxy:    os.Getenv("TRUST_PROXY") == "true",
	}
	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	if cfg.JWTSecret == "" {
		return Config{}, errors.New("JWT_SECRET is required")
	}
	if len(cfg.JWTSecret) < minJWTSecretLen {
		return Config{}, errors.New("JWT_SECRET must be at least 32 characters")
	}
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = ":8080"
	}
	return cfg, nil
}
