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

func TestApiKeyMigration(t *testing.T) {
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

	// api_key table must exist.
	var reg string
	if err := db.QueryRowContext(ctx, "SELECT to_regclass($1)", "api_key").Scan(&reg); err != nil {
		t.Fatalf("check table api_key: %v", err)
	}
	if reg != "api_key" {
		t.Fatalf("table api_key missing after migration")
	}

	// Hash uniqueness is enforced.
	// First, create a project and environment (required for FK constraints).
	var projectID, envID string
	if err := db.QueryRowContext(ctx, "INSERT INTO project(name, slug) VALUES ($1, $2) RETURNING id", "test", "test").Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := db.QueryRowContext(ctx, "INSERT INTO environment(project_id, name) VALUES ($1, $2) RETURNING id", projectID, "test").Scan(&envID); err != nil {
		t.Fatalf("create environment: %v", err)
	}

	// Insert first api_key with a specific hash.
	if _, err := db.ExecContext(ctx,
		"INSERT INTO api_key(environment_id, project_id, name, hash) VALUES ($1, $2, $3, $4)",
		envID, projectID, "key1", "abc123def456"); err != nil {
		t.Fatalf("first insert: %v", err)
	}

	// Insert second api_key with the same hash should fail.
	if _, err := db.ExecContext(ctx,
		"INSERT INTO api_key(environment_id, project_id, name, hash) VALUES ($1, $2, $3, $4)",
		envID, projectID, "key2", "abc123def456"); err == nil {
		t.Fatal("expected duplicate hash to be rejected")
	}
}
