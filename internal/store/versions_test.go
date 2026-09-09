//go:build integration

package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/Rohan-Muslekar/knobs/internal/store"
)

func TestVersionsLifecycle(t *testing.T) {
	ctx := context.Background()
	repo := store.New(migratedPool(t))

	p, err := repo.CreateProject(ctx, repo.Pool(), "Acme", "acme")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	env, err := repo.CreateEnvironment(ctx, repo.Pool(), p.ID, "staging")
	if err != nil {
		t.Fatalf("create environment: %v", err)
	}
	if _, err := repo.GetOrInitSchema(ctx, repo.Pool(), p.ID); err != nil {
		t.Fatalf("init schema: %v", err)
	}

	n, err := repo.NextVersionNumber(ctx, repo.Pool(), env.ID)
	if err != nil {
		t.Fatalf("next version (fresh env): %v", err)
	}
	if n != 1 {
		t.Fatalf("next version (fresh env) = %d, want 1", n)
	}

	v1, err := repo.InsertVersion(ctx, repo.Pool(), env.ID, n, 1, map[string]any{"maxRetries": float64(3)}, uuid.Nil)
	if err != nil {
		t.Fatalf("insert v1: %v", err)
	}
	if v1.Version != 1 {
		t.Fatalf("v1.Version = %d, want 1", v1.Version)
	}
	if v1.CreatedBy != nil {
		t.Fatalf("v1.CreatedBy = %v, want nil (createdBy passed as uuid.Nil)", v1.CreatedBy)
	}
	if err := repo.SetCurrentVersion(ctx, repo.Pool(), env.ID, v1.ID); err != nil {
		t.Fatalf("set current v1: %v", err)
	}

	cur, err := repo.CurrentVersion(ctx, repo.Pool(), env.ID)
	if err != nil {
		t.Fatalf("current version: %v", err)
	}
	if cur.Version != 1 {
		t.Fatalf("current.Version = %d, want 1", cur.Version)
	}
	if got, _ := cur.Values["maxRetries"].(float64); got != 3 {
		t.Fatalf("current.Values[maxRetries] = %v, want 3", cur.Values["maxRetries"])
	}

	n2, err := repo.NextVersionNumber(ctx, repo.Pool(), env.ID)
	if err != nil {
		t.Fatalf("next version (after v1): %v", err)
	}
	if n2 != 2 {
		t.Fatalf("next version (after v1) = %d, want 2", n2)
	}

	v2, err := repo.InsertVersion(ctx, repo.Pool(), env.ID, n2, 1, map[string]any{"maxRetries": float64(4)}, uuid.Nil)
	if err != nil {
		t.Fatalf("insert v2: %v", err)
	}
	if err := repo.SetCurrentVersion(ctx, repo.Pool(), env.ID, v2.ID); err != nil {
		t.Fatalf("set current v2: %v", err)
	}

	// History is immutable: version 1 must still return its original values
	// even after the environment's current pointer has moved to v2.
	old, err := repo.VersionByNumber(ctx, repo.Pool(), env.ID, 1)
	if err != nil {
		t.Fatalf("version by number (1): %v", err)
	}
	if got, _ := old.Values["maxRetries"].(float64); got != 3 {
		t.Fatalf("version 1 values[maxRetries] = %v, want 3 (unchanged by v2)", old.Values["maxRetries"])
	}

	if _, err := repo.VersionByNumber(ctx, repo.Pool(), env.ID, 99); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("version by number (missing) err = %v, want ErrNotFound", err)
	}

	n3, err := repo.NextVersionNumber(ctx, repo.Pool(), env.ID)
	if err != nil {
		t.Fatalf("next version (after v2): %v", err)
	}
	if n3 != 3 {
		t.Fatalf("next version (after v2) = %d, want 3", n3)
	}
}
