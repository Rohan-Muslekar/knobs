package knobs

// This file is a stdlib-only reimplementation of the delta wire types and
// apply logic in internal/delivery/delta.go. sdk/go is a separate Go module
// and cannot import that internal package, so the shapes and semantics here
// are mirrored by hand — kept in lockstep with the reference implementation
// (and the TS SDK's src/delta.ts) via testdata/delta-vectors.json, copied
// byte-for-byte from internal/delivery/testdata/delta-vectors.json.

// FieldDelta describes the change to a Snapshot's Values between two
// revisions: Set carries keys that are new or whose value changed, Remove
// carries keys that existed before and are gone now. Both are omitted on the
// wire (and treated as empty here) when there's nothing to report on that
// side.
type FieldDelta struct {
	Set    map[string]any `json:"set,omitempty"`
	Remove []string       `json:"remove,omitempty"`
}

// TargetingDelta is the same idea as FieldDelta, but for a Snapshot's
// targeting rules.
type TargetingDelta struct {
	Set    map[string][]Rule `json:"set,omitempty"`
	Remove []string          `json:"remove,omitempty"`
}

// Delta is what the SSE stream sends instead of a full Snapshot once a
// client has already seen the revision named by From: only the values and
// targeting rules that changed. Type is always "delta", so a client can
// distinguish it from a full "snapshot" frame on the same stream.
type Delta struct {
	Type       string         `json:"type"`
	Version    int            `json:"version"`
	Revision   int64          `json:"revision"`
	From       int64          `json:"from"`
	SchemaHash string         `json:"schemaHash"`
	Values     FieldDelta     `json:"values"`
	Targeting  TargetingDelta `json:"targeting"`
}

// applyDelta applies d on top of current and returns the resulting
// Snapshot: a fresh copy of current.Values/current.Targeting with d's Set
// entries added/overwritten and d's Remove entries deleted, plus
// Version/Revision/SchemaHash taken from d. It is pure — current (and its
// maps) is never mutated, so a caller reading current concurrently under
// its own lock is unaffected — and doesn't itself check d.From against
// current.Revision; the caller decides whether the delta is safe to apply
// (see Client.applyDeltaFrame) and falls back to a full resync otherwise.
// An absent Set/Remove on either side is simply empty, matching the wire
// contract.
func applyDelta(current Snapshot, d Delta) Snapshot {
	values := make(map[string]any, len(current.Values)+len(d.Values.Set))
	for k, v := range current.Values {
		values[k] = v
	}
	for k, v := range d.Values.Set {
		values[k] = v
	}
	for _, k := range d.Values.Remove {
		delete(values, k)
	}

	targeting := make(map[string][]Rule, len(current.Targeting)+len(d.Targeting.Set))
	for k, v := range current.Targeting {
		targeting[k] = v
	}
	for k, v := range d.Targeting.Set {
		targeting[k] = v
	}
	for _, k := range d.Targeting.Remove {
		delete(targeting, k)
	}

	return Snapshot{
		Version:    d.Version,
		Revision:   d.Revision,
		SchemaHash: d.SchemaHash,
		Values:     values,
		Targeting:  targeting,
	}
}
