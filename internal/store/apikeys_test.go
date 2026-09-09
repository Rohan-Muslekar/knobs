//go:build integration

package store_test

import (
	"context"
	"testing"

	"github.com/Rohan-Muslekar/knobs/internal/store"
)

func TestApiKeyCRUD(t *testing.T) {
	ctx := context.Background()
	repo := store.New(migratedPool(t))

	p, err := repo.CreateProject(ctx, repo.Pool(), "Acme", "acme")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	e, err := repo.CreateEnvironment(ctx, repo.Pool(), p.ID, "staging")
	if err != nil {
		t.Fatalf("create environment: %v", err)
	}

	const hash = "deadbeefcafef00d"
	created, err := repo.CreateApiKey(ctx, repo.Pool(), e.ID, p.ID, "ci key", hash)
	if err != nil {
		t.Fatalf("create api key: %v", err)
	}
	if created.EnvironmentID != e.ID {
		t.Fatalf("EnvironmentID = %v, want %v", created.EnvironmentID, e.ID)
	}
	if created.ProjectID != p.ID {
		t.Fatalf("ProjectID = %v, want %v", created.ProjectID, p.ID)
	}
	if created.Name != "ci key" {
		t.Fatalf("Name = %q, want %q", created.Name, "ci key")
	}
	if created.Scope != "read" {
		t.Fatalf("Scope = %q, want %q", created.Scope, "read")
	}
	if created.LastUsedAt != nil {
		t.Fatalf("LastUsedAt = %v, want nil on a fresh key", created.LastUsedAt)
	}

	// ApiKeyByHash round-trip.
	byHash, err := repo.ApiKeyByHash(ctx, repo.Pool(), hash)
	if err != nil {
		t.Fatalf("api key by hash: %v", err)
	}
	if byHash.ID != created.ID || byHash.EnvironmentID != e.ID || byHash.ProjectID != p.ID || byHash.Name != "ci key" {
		t.Fatalf("byHash = %+v, want match for %+v", byHash, created)
	}

	// A miss maps to store.ErrNotFound.
	if _, err := repo.ApiKeyByHash(ctx, repo.Pool(), "no-such-hash"); err != store.ErrNotFound {
		t.Fatalf("api key by hash (miss) err = %v, want store.ErrNotFound", err)
	}

	// ListApiKeys returns the one key for this environment.
	list, err := repo.ListApiKeys(ctx, repo.Pool(), e.ID)
	if err != nil {
		t.Fatalf("list api keys: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("list api keys len = %d, want 1", len(list))
	}

	// TouchApiKey sets last_used_at.
	if err := repo.TouchApiKey(ctx, repo.Pool(), created.ID); err != nil {
		t.Fatalf("touch api key: %v", err)
	}
	touched, err := repo.ApiKeyByHash(ctx, repo.Pool(), hash)
	if err != nil {
		t.Fatalf("api key by hash (after touch): %v", err)
	}
	if touched.LastUsedAt == nil {
		t.Fatal("LastUsedAt = nil after touch, want non-nil")
	}

	// RevokeApiKey removes the row; a second revoke of the same id is a no-op error.
	if err := repo.RevokeApiKey(ctx, repo.Pool(), created.ID, e.ID); err != nil {
		t.Fatalf("revoke api key: %v", err)
	}
	if _, err := repo.ApiKeyByHash(ctx, repo.Pool(), hash); err != store.ErrNotFound {
		t.Fatalf("api key by hash (after revoke) err = %v, want store.ErrNotFound", err)
	}
	if err := repo.RevokeApiKey(ctx, repo.Pool(), created.ID, e.ID); err != store.ErrNotFound {
		t.Fatalf("revoke nonexistent key err = %v, want store.ErrNotFound", err)
	}
}
