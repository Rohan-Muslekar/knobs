//go:build integration

package store_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/Rohan-Muslekar/knobs/internal/store"
)

func TestListAudit(t *testing.T) {
	ctx := context.Background()
	repo := store.New(migratedPool(t))

	p, err := repo.CreateProject(ctx, repo.Pool(), "Acme", "acme")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	if err := repo.RecordAudit(ctx, repo.Pool(), p.ID, uuid.Nil, "project.create", p.ID.String(), map[string]any{"name": "Acme"}); err != nil {
		t.Fatalf("record audit (create): %v", err)
	}
	if _, err := repo.UpdateProjectName(ctx, repo.Pool(), p.ID, "Acme Inc"); err != nil {
		t.Fatalf("rename project: %v", err)
	}
	if err := repo.RecordAudit(ctx, repo.Pool(), p.ID, uuid.Nil, "project.rename", p.ID.String(), map[string]any{"name": "Acme Inc"}); err != nil {
		t.Fatalf("record audit (rename): %v", err)
	}

	entries, err := repo.ListAudit(ctx, repo.Pool(), p.ID, 100)
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("list audit len = %d, want 2", len(entries))
	}

	// Newest first: the rename (recorded second) must come before the create.
	if entries[0].Action != "project.rename" {
		t.Fatalf("entries[0].Action = %q, want %q", entries[0].Action, "project.rename")
	}
	if entries[1].Action != "project.create" {
		t.Fatalf("entries[1].Action = %q, want %q", entries[1].Action, "project.create")
	}
	if entries[0].Diff == nil {
		t.Fatal("entries[0].Diff = nil, want raw JSON")
	}
	if entries[0].ProjectID == nil || *entries[0].ProjectID != p.ID {
		t.Fatalf("entries[0].ProjectID = %v, want %v", entries[0].ProjectID, p.ID)
	}
	if entries[0].Actor != nil {
		t.Fatalf("entries[0].Actor = %v, want nil (recorded with uuid.Nil)", entries[0].Actor)
	}

	// Limit is respected.
	limited, err := repo.ListAudit(ctx, repo.Pool(), p.ID, 1)
	if err != nil {
		t.Fatalf("list audit (limit 1): %v", err)
	}
	if len(limited) != 1 {
		t.Fatalf("list audit (limit 1) len = %d, want 1", len(limited))
	}
	if limited[0].Action != "project.rename" {
		t.Fatalf("limited[0].Action = %q, want %q", limited[0].Action, "project.rename")
	}

	// A different project's audit entries are not returned.
	other, err := repo.CreateProject(ctx, repo.Pool(), "Other", "other")
	if err != nil {
		t.Fatalf("create other project: %v", err)
	}
	empty, err := repo.ListAudit(ctx, repo.Pool(), other.ID, 100)
	if err != nil {
		t.Fatalf("list audit (other project): %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("list audit (other project) len = %d, want 0", len(empty))
	}
}
