//go:build integration

package api_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/Rohan-Muslekar/knobs/internal/store"
)

// migratedPool starts a throwaway Postgres, applies all migrations, and
// returns a live pgx pool. The container is terminated on test cleanup.
//
// Identical implementation to internal/store/testhelper_test.go, duplicated
// because test helpers do not cross package boundaries.
func migratedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, _ := migratedPoolAndURL(t)
	return pool
}

// migratedPoolAndURL is migratedPool plus the raw connection string, needed
// by callers that must open their own connection outside the pool — e.g. a
// delivery.Listener, which LISTENs on a dedicated, non-pooled pgx.Conn.
func migratedPoolAndURL(t *testing.T) (*pgxpool.Pool, string) {
	t.Helper()
	ctx := context.Background()
	pg, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("knobs"),
		tcpostgres.WithUsername("knobs"),
		tcpostgres.WithPassword("knobs"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(pg) })

	url, err := pg.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connstring: %v", err)
	}

	sqlDB, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := store.RunMigrations(sqlDB); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	_ = sqlDB.Close()

	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool, url
}
