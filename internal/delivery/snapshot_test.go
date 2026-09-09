package delivery

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Rohan-Muslekar/knobs/internal/schema"
	"github.com/Rohan-Muslekar/knobs/internal/store"
)

func TestSchemaHash_StableForSameInput(t *testing.T) {
	def := schema.Definition{Fields: []schema.Field{
		{Name: "timeout", Type: "duration"},
		{Name: "retries", Type: "int"},
	}}
	h1 := SchemaHash(def)
	h2 := SchemaHash(def)
	if h1 != h2 {
		t.Fatalf("SchemaHash not stable: %s != %s", h1, h2)
	}
	if h1 == "" {
		t.Fatal("SchemaHash returned empty string")
	}
}

func TestSchemaHash_OrderIndependent(t *testing.T) {
	a := schema.Definition{Fields: []schema.Field{
		{Name: "timeout", Type: "duration"},
		{Name: "retries", Type: "int"},
	}}
	b := schema.Definition{Fields: []schema.Field{
		{Name: "retries", Type: "int"},
		{Name: "timeout", Type: "duration"},
	}}
	if SchemaHash(a) != SchemaHash(b) {
		t.Fatalf("SchemaHash depends on field insertion order: %s != %s", SchemaHash(a), SchemaHash(b))
	}
}

func TestSchemaHash_DiffersWhenFieldChanges(t *testing.T) {
	a := schema.Definition{Fields: []schema.Field{
		{Name: "retries", Type: "int"},
	}}
	b := schema.Definition{Fields: []schema.Field{
		{Name: "retries", Type: "float"},
	}}
	if SchemaHash(a) == SchemaHash(b) {
		t.Fatalf("SchemaHash did not change when a field's type changed: %s", SchemaHash(a))
	}

	c := schema.Definition{Fields: []schema.Field{
		{Name: "retries", Type: "int"},
		{Name: "timeout", Type: "duration"},
	}}
	if SchemaHash(a) == SchemaHash(c) {
		t.Fatalf("SchemaHash did not change when a field was added: %s", SchemaHash(a))
	}
}

func TestBuildSnapshot_MapsVersionValuesAndHash(t *testing.T) {
	def := schema.Definition{Fields: []schema.Field{
		{Name: "retries", Type: "int"},
	}}
	cv := store.ConfigVersion{
		ID:            uuid.New(),
		EnvironmentID: uuid.New(),
		Version:       3,
		SchemaVersion: 1,
		Values:        map[string]any{"retries": float64(5)},
		CreatedAt:     time.Now(),
	}

	snap := BuildSnapshot(cv, def, 42)

	if snap.Version != cv.Version {
		t.Fatalf("Version = %d, want %d", snap.Version, cv.Version)
	}
	if snap.Revision != 42 {
		t.Fatalf("Revision = %d, want 42", snap.Revision)
	}
	if snap.SchemaHash != SchemaHash(def) {
		t.Fatalf("SchemaHash = %s, want %s", snap.SchemaHash, SchemaHash(def))
	}
	if len(snap.Values) != 1 || snap.Values["retries"] != float64(5) {
		t.Fatalf("Values = %v, want {retries: 5}", snap.Values)
	}
}

func TestBuildSnapshot_NilValuesBecomeEmptyMap(t *testing.T) {
	def := schema.Definition{}
	cv := store.ConfigVersion{Version: 1, Values: nil}

	snap := BuildSnapshot(cv, def, 0)

	if snap.Values == nil {
		t.Fatal("Values is nil, want empty map")
	}
	if len(snap.Values) != 0 {
		t.Fatalf("Values = %v, want empty", snap.Values)
	}
}
