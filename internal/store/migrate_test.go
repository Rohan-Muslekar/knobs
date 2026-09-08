//go:build integration

package store_test

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/Rohan-Muslekar/knobs/internal/store"
)

func TestRunMigrations(t *testing.T) {
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

	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := store.RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}

	var value string
	err = db.QueryRowContext(ctx,
		"SELECT value FROM knobs_meta WHERE key = 'schema_baseline'").Scan(&value)
	if err != nil {
		t.Fatalf("query knobs_meta: %v", err)
	}
	if value != "p0" {
		t.Fatalf("schema_baseline = %q, want p0", value)
	}
}
