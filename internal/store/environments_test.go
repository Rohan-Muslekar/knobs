//go:build integration

package store_test

import (
	"context"
	"testing"

	"github.com/Rohan-Muslekar/knobs/internal/store"
)

func TestEnvironmentCRUD(t *testing.T) {
	ctx := context.Background()
	repo := store.New(migratedPool(t))

	p, err := repo.CreateProject(ctx, repo.Pool(), "Acme", "acme")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	e1, err := repo.CreateEnvironment(ctx, repo.Pool(), p.ID, "staging")
	if err != nil {
		t.Fatalf("create env 1: %v", err)
	}
	if e1.CurrentVersionID != nil {
		t.Fatalf("CurrentVersionID = %v, want nil on fresh env", e1.CurrentVersionID)
	}

	e2, err := repo.CreateEnvironment(ctx, repo.Pool(), p.ID, "production")
	if err != nil {
		t.Fatalf("create env 2: %v", err)
	}

	list, err := repo.ListEnvironments(ctx, repo.Pool(), p.ID)
	if err != nil || len(list) != 2 {
		t.Fatalf("list len=%d err=%v", len(list), err)
	}

	// duplicate name within the same project rejected by the unique constraint
	if _, err := repo.CreateEnvironment(ctx, repo.Pool(), p.ID, "staging"); err == nil {
		t.Fatal("expected duplicate name rejection")
	}

	got, err := repo.EnvironmentByID(ctx, repo.Pool(), e1.ID)
	if err != nil || got.Name != "staging" || got.ProjectID != p.ID {
		t.Fatalf("byID: %+v err=%v", got, err)
	}

	if _, err := repo.EnvironmentByID(ctx, repo.Pool(), e2.ID); err != nil {
		t.Fatalf("byID e2: %v", err)
	}
}
