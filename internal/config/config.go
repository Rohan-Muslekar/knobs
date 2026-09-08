// Package config loads server settings from the environment.
package config

import (
	"errors"
	"os"
)

// Config holds server runtime settings. Knobs never bootstraps its own config
// from itself; these come from the process environment.
type Config struct {
	DatabaseURL string
	ListenAddr  string
}

// Load reads settings from environment variables and applies defaults.
func Load() (Config, error) {
	cfg := Config{
		DatabaseURL: os.Getenv("DATABASE_URL"),
		ListenAddr:  os.Getenv("LISTEN_ADDR"),
	}
	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = ":8080"
	}
	return cfg, nil
}
