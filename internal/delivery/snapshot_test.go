package delivery

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Rohan-Muslekar/knobs/internal/schema"
	"github.com/Rohan-Muslekar/knobs/internal/store"
	"github.com/Rohan-Muslekar/knobs/internal/targeting"
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

func TestBuildSnapshot_CopiesTargeting(t *testing.T) {
	def := schema.Definition{Fields: []schema.Field{
		{Name: "maxRetries", Type: "int"},
	}}
	tgt := targeting.Map{
		"maxRetries": []targeting.Rule{
			{Value: float64(7)},
		},
	}
	cv := store.ConfigVersion{Version: 1, Targeting: tgt}

	snap := BuildSnapshot(cv, def, 0)

	if len(snap.Targeting) != 1 || len(snap.Targeting["maxRetries"]) != 1 {
		t.Fatalf("Targeting = %v, want %v", snap.Targeting, tgt)
	}

	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"targeting"`) {
		t.Fatalf("marshaled snapshot missing targeting field: %s", raw)
	}

	var round Snapshot
	if err := json.Unmarshal(raw, &round); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(round.Targeting) != 1 || len(round.Targeting["maxRetries"]) != 1 {
		t.Fatalf("round-tripped Targeting = %v, want %v", round.Targeting, tgt)
	}
	if round.Targeting["maxRetries"][0].Value != float64(7) {
		t.Fatalf("round-tripped rule value = %v, want 7", round.Targeting["maxRetries"][0].Value)
	}
}

func TestBuildSnapshot_NilTargetingOmittedFromJSON(t *testing.T) {
	def := schema.Definition{}
	cv := store.ConfigVersion{Version: 1, Targeting: nil}

	snap := BuildSnapshot(cv, def, 0)

	if snap.Targeting != nil {
		t.Fatalf("Targeting = %v, want nil", snap.Targeting)
	}

	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(raw), "targeting") {
		t.Fatalf("marshaled snapshot with nil Targeting should omit the field entirely: %s", raw)
	}
}
