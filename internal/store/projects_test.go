//go:build integration

package store_test

import (
	"context"
	"testing"

	"github.com/Rohan-Muslekar/knobs/internal/store"
)

func TestProjectCRUD(t *testing.T) {
	ctx := context.Background()
	repo := store.New(migratedPool(t))

	p, err := repo.CreateProject(ctx, repo.Pool(), "Acme", "acme")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := repo.ProjectByID(ctx, repo.Pool(), p.ID)
	if err != nil || got.Slug != "acme" {
		t.Fatalf("byID: %+v err=%v", got, err)
	}
	upd, err := repo.UpdateProjectName(ctx, repo.Pool(), p.ID, "Acme Inc")
	if err != nil || upd.Name != "Acme Inc" {
		t.Fatalf("update: %+v err=%v", upd, err)
	}
	list, err := repo.ListProjects(ctx, repo.Pool())
	if err != nil || len(list) != 1 {
		t.Fatalf("list len=%d err=%v", len(list), err)
	}
	// duplicate slug rejected by the unique constraint
	if _, err := repo.CreateProject(ctx, repo.Pool(), "Other", "acme"); err == nil {
		t.Fatal("expected duplicate slug rejection")
	}
}
