// Package delivery assembles the read-only artifacts served to machine
// consumers (SDKs) over the API-key-guarded routes: the config snapshot
// today, and later the SSE stream that pushes fresh ones.
package delivery

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"github.com/Rohan-Muslekar/knobs/internal/schema"
	"github.com/Rohan-Muslekar/knobs/internal/store"
)

// SchemaHash returns the sha256 hex digest of def's canonical JSON, so an SDK
// can detect drift by comparing hashes rather than diffing full documents.
// "Canonical" here means the hash depends only on the set of fields and their
// content, not the order they were inserted in: fields are sorted by name
// before marshaling, since json.Marshal already emits a struct's own fields
// in a fixed order.
func SchemaHash(def schema.Definition) string {
	sorted := make([]schema.Field, len(def.Fields))
	copy(sorted, def.Fields)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	raw, err := json.Marshal(schema.Definition{Fields: sorted})
	if err != nil {
		// schema.Definition holds only JSON-marshalable data (strings, numbers,
		// bools, slices of the same) — Marshal failing here would mean the type
		// grew a field that can't round-trip, which is a bug worth surfacing
		// loudly rather than papering over with a zero-value hash.
		panic("delivery: schema.Definition failed to marshal: " + err.Error())
	}

	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// Snapshot is what delivery routes hand to machine consumers: the config
// version, its values, and a hash of the schema they were validated against.
type Snapshot struct {
	Version    int            `json:"version"`
	SchemaHash string         `json:"schemaHash"`
	Values     map[string]any `json:"values"`
}

// BuildSnapshot assembles a Snapshot from an environment's current config
// version and its project's schema definition.
func BuildSnapshot(current store.ConfigVersion, def schema.Definition) Snapshot {
	values := current.Values
	if values == nil {
		values = map[string]any{}
	}
	return Snapshot{
		Version:    current.Version,
		SchemaHash: SchemaHash(def),
		Values:     values,
	}
}
