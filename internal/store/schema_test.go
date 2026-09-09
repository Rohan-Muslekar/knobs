//go:build integration

package store_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/Rohan-Muslekar/knobs/internal/schema"
	"github.com/Rohan-Muslekar/knobs/internal/store"
)

func TestGetOrInitSchemaAndUpdate(t *testing.T) {
	ctx := context.Background()
	repo := store.New(migratedPool(t))

	p, err := repo.CreateProject(ctx, repo.Pool(), "Acme", "acme")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	cs, err := repo.GetOrInitSchema(ctx, repo.Pool(), p.ID)
	if err != nil {
		t.Fatalf("get or init: %v", err)
	}
	if len(cs.Definition.Fields) != 0 {
		t.Fatalf("fresh definition fields = %v, want empty", cs.Definition.Fields)
	}
	if cs.SchemaVersion != 1 {
		t.Fatalf("fresh schema_version = %d, want 1", cs.SchemaVersion)
	}

	// Calling again must not clobber (upsert is insert-if-absent).
	cs2, err := repo.GetOrInitSchema(ctx, repo.Pool(), p.ID)
	if err != nil {
		t.Fatalf("get or init (2nd): %v", err)
	}
	if cs2.SchemaVersion != 1 {
		t.Fatalf("2nd get schema_version = %d, want 1 (unchanged)", cs2.SchemaVersion)
	}

	f5 := 5.0
	def := schema.Definition{Fields: []schema.Field{
		{Name: "maxRetries", Type: "int", Required: true, Max: &f5},
		{Name: "featureX", Type: "bool"},
	}}
	updated, err := repo.UpdateSchema(ctx, repo.Pool(), p.ID, def, uuid.New())
	if err != nil {
		t.Fatalf("update schema: %v", err)
	}
	if updated.SchemaVersion != 2 {
		t.Fatalf("updated schema_version = %d, want 2", updated.SchemaVersion)
	}
	if len(updated.Definition.Fields) != 2 {
		t.Fatalf("updated fields = %v, want 2 entries", updated.Definition.Fields)
	}
	if updated.Definition.Fields[0].Name != "maxRetries" || updated.Definition.Fields[0].Type != "int" {
		t.Fatalf("round-tripped field = %+v", updated.Definition.Fields[0])
	}

	// GetOrInitSchema on an existing row must not re-init/reset it.
	cs3, err := repo.GetOrInitSchema(ctx, repo.Pool(), p.ID)
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	if cs3.SchemaVersion != 2 || len(cs3.Definition.Fields) != 2 {
		t.Fatalf("get after update = %+v, want version 2 with 2 fields", cs3)
	}
}
