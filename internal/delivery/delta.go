package delivery

import (
	"encoding/json"
	"reflect"
	"sort"

	"github.com/Rohan-Muslekar/knobs/internal/targeting"
)

// FieldDelta describes the change to a Snapshot's Values between two
// revisions: Set carries keys that are new or whose value changed, Remove
// carries keys that existed before and are gone now.
type FieldDelta struct {
	Set    map[string]any `json:"set,omitempty"`
	Remove []string       `json:"remove,omitempty"`
}

// TargetingDelta is the same idea as FieldDelta, but for a Snapshot's
// targeting rules.
type TargetingDelta struct {
	Set    map[string][]targeting.Rule `json:"set,omitempty"`
	Remove []string                    `json:"remove,omitempty"`
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

// ComputeDelta diffs old against new and returns the Delta that turns old
// into new: a key present in new whose value isn't equal to old's (or
// absent from old entirely) lands in Set; a key present in old but gone
// from new lands in Remove. Values and Targeting are diffed independently
// by the same rule. The result is deterministic — Remove slices are
// sorted — so two calls with the same inputs always produce byte-identical
// JSON.
func ComputeDelta(old, new Snapshot) Delta {
	d := Delta{
		Type:       "delta",
		Version:    new.Version,
		Revision:   new.Revision,
		From:       old.Revision,
		SchemaHash: new.SchemaHash,
	}

	d.Values.Set, d.Values.Remove = diffValues(old.Values, new.Values)
	d.Targeting.Set, d.Targeting.Remove = diffTargeting(old.Targeting, new.Targeting)

	return d
}

func diffValues(old, new map[string]any) (set map[string]any, remove []string) {
	for k, nv := range new {
		ov, ok := old[k]
		if !ok || !reflect.DeepEqual(ov, nv) {
			if set == nil {
				set = map[string]any{}
			}
			set[k] = nv
		}
	}
	for k := range old {
		if _, ok := new[k]; !ok {
			remove = append(remove, k)
		}
	}
	sort.Strings(remove)
	return set, remove
}

func diffTargeting(old, new targeting.Map) (set map[string][]targeting.Rule, remove []string) {
	for k, nv := range new {
		ov, ok := old[k]
		if !ok || !reflect.DeepEqual(ov, nv) {
			if set == nil {
				set = map[string][]targeting.Rule{}
			}
			set[k] = nv
		}
	}
	for k := range old {
		if _, ok := new[k]; !ok {
			remove = append(remove, k)
		}
	}
	sort.Strings(remove)
	return set, remove
}

// ApplyDelta applies d on top of base and returns the resulting Snapshot.
// If d.From doesn't match base.Revision — meaning d wasn't computed
// against the revision the caller is holding — it returns (base, false)
// and leaves base untouched. Otherwise it returns (result, true), where
// result is a copy of base with d's Set entries added/overwritten, d's
// Remove entries deleted, and Version/Revision/SchemaHash updated to d's.
// base's own maps are never mutated in place; the caller may still be
// holding and reading base concurrently.
func ApplyDelta(base Snapshot, d Delta) (Snapshot, bool) {
	if d.From != base.Revision {
		return base, false
	}

	result := base
	result.Version = d.Version
	result.Revision = d.Revision
	result.SchemaHash = d.SchemaHash
	result.Values = applyFieldDelta(base.Values, d.Values)
	result.Targeting = applyTargetingDelta(base.Targeting, d.Targeting)

	return result, true
}

func applyFieldDelta(base map[string]any, d FieldDelta) map[string]any {
	result := make(map[string]any, len(base)+len(d.Set))
	for k, v := range base {
		result[k] = v
	}
	for k, v := range d.Set {
		result[k] = v
	}
	for _, k := range d.Remove {
		delete(result, k)
	}
	return result
}

func applyTargetingDelta(base targeting.Map, d TargetingDelta) targeting.Map {
	if len(base) == 0 && len(d.Set) == 0 {
		return nil
	}
	result := make(targeting.Map, len(base)+len(d.Set))
	for k, v := range base {
		result[k] = v
	}
	for k, v := range d.Set {
		result[k] = v
	}
	for _, k := range d.Remove {
		delete(result, k)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// fullFrame wraps a Snapshot with the "type":"snapshot" discriminator the
// delta-mode stream adds to full frames, without touching Snapshot's own
// json tags — the non-delta stream path must keep emitting bare snapshots.
type fullFrame struct {
	Type string `json:"type"`
	Snapshot
}

// FullFrameJSON marshals s the same way json.Marshal(s) would, but with a
// leading "type":"snapshot" field, for use as the full-frame variant on a
// delta-enabled SSE stream. Every other field keeps the exact same key it
// has on a bare Snapshot.
func FullFrameJSON(s Snapshot) ([]byte, error) {
	return json.Marshal(fullFrame{Type: "snapshot", Snapshot: s})
}
