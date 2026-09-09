// Package config loads server settings from the environment.
package config

import (
	"errors"
	"os"
)

// Config holds server runtime settings. Knobs never bootstraps its own config
// from itself; these come from the process environment.
type Config struct {
	DatabaseURL   string
	ListenAddr    string
	JWTSecret     string
	AdminEmail    string
	AdminPassword string
	CookieSecure  bool
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
	}
	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	if cfg.JWTSecret == "" {
		return Config{}, errors.New("JWT_SECRET is required")
	}
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = ":8080"
	}
	return cfg, nil
}
