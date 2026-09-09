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
	"github.com/Rohan-Muslekar/knobs/internal/auth"
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

	repo := store.New(pool)
	authr := auth.New(cfg.JWTSecret, cfg.CookieSecure)

	if err := seedAdmin(ctx, repo, authr, cfg); err != nil {
		return fmt.Errorf("seed admin: %w", err)
	}

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           api.NewRouter(api.Deps{Repo: repo, Auth: authr}),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	fmt.Printf("knobs listening on %s\n", cfg.ListenAddr)
	return srv.ListenAndServe()
}

// seedAdmin creates the first admin user from ADMIN_EMAIL/ADMIN_PASSWORD when
// the users table is empty. It is a no-op once any user exists or when those
// environment variables are unset.
func seedAdmin(ctx context.Context, repo *store.Repo, authr *auth.Authenticator, cfg config.Config) error {
	if cfg.AdminEmail == "" || cfg.AdminPassword == "" {
		return nil
	}
	n, err := repo.CountUsers(ctx, repo.Pool())
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	hash, err := authr.Hash(cfg.AdminPassword)
	if err != nil {
		return err
	}
	_, err = repo.CreateUser(ctx, repo.Pool(), cfg.AdminEmail, hash)
	return err
}
