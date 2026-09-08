// Package app assembles the Knobs server from its components.
package app

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/Rohan-Muslekar/knobs/internal/api"
	"github.com/Rohan-Muslekar/knobs/internal/config"
	"github.com/Rohan-Muslekar/knobs/internal/store"
)

// NewRouter builds the HTTP handler. This surface (health + embedded UI) needs
// no database, so it is safe to construct in tests without Postgres.
func NewRouter() http.Handler {
	return api.NewRouter(api.Deps{})
}

// Run loads config, connects to Postgres, applies migrations, and serves.
func Run(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	pool, err := store.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connect postgres: %w", err)
	}
	defer pool.Close()

	db, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("open migration db: %w", err)
	}
	defer db.Close()
	if err := store.RunMigrations(db); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           NewRouter(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	fmt.Printf("knobs listening on %s\n", cfg.ListenAddr)
	return srv.ListenAndServe()
}
