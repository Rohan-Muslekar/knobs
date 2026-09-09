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

func TestP1Migration(t *testing.T) {
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
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := store.RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}

	// Every P1 table must exist.
	for _, table := range []string{
		"project", "app_user", "environment", "config_schema", "config_version", "audit_log",
	} {
		var reg string
		if err := db.QueryRowContext(ctx, "SELECT to_regclass($1)", table).Scan(&reg); err != nil {
			t.Fatalf("check table %s: %v", table, err)
		}
		if reg != table {
			t.Fatalf("table %s missing after migration", table)
		}
	}

	// Case-insensitive email uniqueness is enforced.
	if _, err := db.ExecContext(ctx,
		`INSERT INTO app_user(email, password_hash) VALUES ('Admin@x.com', 'h')`); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO app_user(email, password_hash) VALUES ('admin@x.com', 'h2')`); err == nil {
		t.Fatal("expected duplicate email (case-insensitive) to be rejected")
	}
}
