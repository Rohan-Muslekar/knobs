// Package app assembles the Knobs server from its components.
package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/Rohan-Muslekar/knobs/internal/api"
	"github.com/Rohan-Muslekar/knobs/internal/auth"
	"github.com/Rohan-Muslekar/knobs/internal/config"
	"github.com/Rohan-Muslekar/knobs/internal/delivery"
	"github.com/Rohan-Muslekar/knobs/internal/store"
)

// shutdownTimeout bounds how long Run waits, on SIGINT/SIGTERM, for
// in-flight requests — including open SSE streams — to drain before
// forcibly closing them.
const shutdownTimeout = 15 * time.Second

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

	hub := delivery.NewHub()
	listener := delivery.NewListener(cfg.DatabaseURL, hub, snapshotLoader(repo))

	// The Listener has its own lifetime, distinct from the shutdown signal
	// context below: it must keep running for as long as the server might
	// still be draining SSE connections, and is only cancelled once the
	// server has fully stopped.
	listenerCtx, cancelListener := context.WithCancel(context.Background())
	defer cancelListener()
	listenerDone := make(chan struct{})
	go func() {
		defer close(listenerDone)
		listener.Run(listenerCtx)
	}()

	// shutdownSignal is closed (never sent on) right before srv.Shutdown is
	// called below, so every open handleStream connection notices and
	// returns on its own — going idle — rather than sitting there as the
	// "active connection" srv.Shutdown would otherwise have to forcibly
	// wait out until shutdownTimeout. See stream_handler.go's select loop.
	shutdownSignal := make(chan struct{})

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           api.NewRouter(api.Deps{Repo: repo, Auth: authr, Hub: hub, Shutdown: shutdownSignal, TrustProxy: cfg.TrustProxy}),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// WriteTimeout above would otherwise cap every response write,
	// including an SSE stream's — handleStream clears its own write
	// deadline on entry (http.NewResponseController), so a healthy stream
	// is bounded only by client disconnect or shutdownSignal, not by this
	// server-wide timeout meant for ordinary request/response handlers.

	sigCtx, stopSignals := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	serveErr := make(chan error, 1)
	go func() {
		fmt.Printf("knobs listening on %s\n", cfg.ListenAddr)
		serveErr <- srv.ListenAndServe()
	}()

	select {
	case err := <-serveErr:
		// The server stopped on its own (bind failure, etc.), not via a
		// shutdown signal: there is nothing to drain.
		cancelListener()
		<-listenerDone
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-sigCtx.Done():
		// Close shutdownSignal FIRST, before calling srv.Shutdown: this is
		// what makes every open SSE stream's handler return promptly (see
		// stream_handler.go), so by the time Shutdown starts waiting,
		// those connections are already going idle instead of being
		// actively held open — Shutdown only ever waits out idle
		// connections, never tells active handlers to stop on its own.
		close(shutdownSignal)

		shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancelShutdown()
		shutdownErr := srv.Shutdown(shutdownCtx)
		cancelListener()
		<-listenerDone
		<-serveErr // ListenAndServe always returns once Shutdown completes.
		return shutdownErr
	}
}

// snapshotLoader builds the delivery.SnapshotLoader the Listener uses to
// turn a NOTIFY's envID into a fresh delivery.Snapshot, the same assembly
// GET /v1/snapshot performs: current config version + the project's schema.
func snapshotLoader(repo *store.Repo) delivery.SnapshotLoader {
	return func(ctx context.Context, envID uuid.UUID) (delivery.Snapshot, bool) {
		env, err := repo.EnvironmentByID(ctx, repo.Pool(), envID)
		if err != nil {
			log.Printf("delivery: snapshotLoader %s: could not load environment, dropping notification: %v", envID, err)
			return delivery.Snapshot{}, false
		}
		cv, err := repo.CurrentVersion(ctx, repo.Pool(), envID)
		if err != nil {
			log.Printf("delivery: snapshotLoader %s: could not load current version, dropping notification: %v", envID, err)
			return delivery.Snapshot{}, false
		}
		cs, err := repo.GetOrInitSchema(ctx, repo.Pool(), env.ProjectID)
		if err != nil {
			log.Printf("delivery: snapshotLoader %s: could not load schema, dropping notification: %v", envID, err)
			return delivery.Snapshot{}, false
		}
		return delivery.BuildSnapshot(cv, cs.Definition, env.DeliveryRevision), true
	}
}

// seedAdmin creates the first admin user from ADMIN_EMAIL/ADMIN_PASSWORD when
// the users table is empty, and makes them an owner of the "default"
// organization the rbac migration creates. It is a no-op once any user
// exists or when those environment variables are unset.
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
	admin, err := repo.CreateUser(ctx, repo.Pool(), cfg.AdminEmail, hash)
	if err != nil {
		return err
	}
	defaultOrg, err := repo.OrganizationBySlug(ctx, repo.Pool(), "default")
	if err != nil {
		return fmt.Errorf("seed admin: lookup default org: %w", err)
	}
	return repo.AddMember(ctx, repo.Pool(), defaultOrg.ID, admin.ID, "owner")
}
