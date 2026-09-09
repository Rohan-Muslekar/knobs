//go:build integration

package store_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Rohan-Muslekar/knobs/internal/store"
)

// newTestRepo spins a migrated Postgres and returns a Repo + pool. Reused by
// later integration tests in this package.
func newTestRepo(t *testing.T) (*store.Repo, *pgxpool.Pool) {
	t.Helper()
	pool := migratedPool(t) // defined in a shared test helper (see below)
	return store.New(pool), pool
}

func TestUserRoundTrip(t *testing.T) {
	ctx := context.Background()
	repo, pool := newTestRepo(t)

	n, err := repo.CountUsers(ctx, pool)
	if err != nil || n != 0 {
		t.Fatalf("count=%d err=%v, want 0", n, err)
	}

	created, err := repo.CreateUser(ctx, pool, "Admin@x.com", "hash")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if created.ID.String() == "" {
		t.Fatal("expected generated id")
	}

	got, err := repo.UserByEmail(ctx, pool, "admin@x.com") // case-insensitive
	if err != nil {
		t.Fatalf("UserByEmail: %v", err)
	}
	if got.ID != created.ID {
		t.Fatalf("id mismatch: %s vs %s", got.ID, created.ID)
	}

	if _, err := repo.UserByEmail(ctx, pool, "nobody@x.com"); err != store.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
