package knobs

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func TestApplyDeltaMergesValuesAndTargeting(t *testing.T) {
	current := Snapshot{
		Version:    1,
		Revision:   5,
		SchemaHash: "h1",
		Values:     map[string]any{"a": 1.0, "b": 2.0},
		Targeting:  map[string][]Rule{"flag": {{Value: false}}},
	}
	d := Delta{
		Type:       "delta",
		Version:    2,
		Revision:   6,
		From:       5,
		SchemaHash: "h2",
		Values: FieldDelta{
			Set:    map[string]any{"a": 10.0, "c": 3.0},
			Remove: []string{"b"},
		},
		Targeting: TargetingDelta{
			Set:    map[string][]Rule{"flag": {{Value: true}}, "new": {{Value: "x"}}},
			Remove: []string{"old"},
		},
	}

	result := applyDelta(current, d)

	wantValues := map[string]any{"a": 10.0, "c": 3.0}
	if !reflect.DeepEqual(result.Values, wantValues) {
		t.Errorf("Values = %#v, want %#v", result.Values, wantValues)
	}
	wantTargeting := map[string][]Rule{"flag": {{Value: true}}, "new": {{Value: "x"}}}
	if !reflect.DeepEqual(result.Targeting, wantTargeting) {
		t.Errorf("Targeting = %#v, want %#v", result.Targeting, wantTargeting)
	}
	if result.Version != 2 || result.Revision != 6 || result.SchemaHash != "h2" {
		t.Errorf("Version/Revision/SchemaHash = %d/%d/%s, want 2/6/h2", result.Version, result.Revision, result.SchemaHash)
	}

	// current must be untouched by the call...
	if len(current.Values) != 2 || current.Values["a"] != 1.0 || current.Values["b"] != 2.0 {
		t.Errorf("applyDelta mutated current.Values: %#v", current.Values)
	}
	if len(current.Targeting) != 1 || len(current.Targeting["flag"]) != 1 || current.Targeting["flag"][0].Value != false {
		t.Errorf("applyDelta mutated current.Targeting: %#v", current.Targeting)
	}

	// ...and result holds its own maps, not aliases of current's.
	result.Values["z"] = "mutated-after-the-fact"
	if _, ok := current.Values["z"]; ok {
		t.Error("result.Values shares backing storage with current.Values")
	}
	result.Targeting["z"] = []Rule{{Value: "mutated-after-the-fact"}}
	if _, ok := current.Targeting["z"]; ok {
		t.Error("result.Targeting shares backing storage with current.Targeting")
	}
}

func TestApplyDeltaAbsentSetRemoveIsNoChange(t *testing.T) {
	current := Snapshot{
		Version:    1,
		Revision:   5,
		SchemaHash: "h1",
		Values:     map[string]any{"a": 1.0},
		Targeting:  map[string][]Rule{"flag": {{Value: false}}},
	}
	// Values and Targeting are their zero value (FieldDelta{}/TargetingDelta{}) —
	// absent Set/Remove on the wire, which must mean "no change" on that side.
	d := Delta{Type: "delta", Version: 1, Revision: 6, From: 5, SchemaHash: "h1"}

	result := applyDelta(current, d)

	if !reflect.DeepEqual(result.Values, current.Values) {
		t.Errorf("Values = %#v, want unchanged %#v", result.Values, current.Values)
	}
	if !reflect.DeepEqual(result.Targeting, current.Targeting) {
		t.Errorf("Targeting = %#v, want unchanged %#v", result.Targeting, current.Targeting)
	}
	if result.Revision != 6 || result.SchemaHash != "h1" {
		t.Errorf("Revision/SchemaHash = %d/%s, want 6/h1", result.Revision, result.SchemaHash)
	}
}

func TestApplyDeltaDoesNotItselfCheckFrom(t *testing.T) {
	// applyDelta is documented as pure and not responsible for the
	// from-vs-current.Revision gate — that's the caller's job
	// (Client.applyDeltaFrame decides whether to call this at all, and
	// falls back to a full resync when From doesn't chain). A mismatched
	// From here must still apply mechanically, so this pins that contract:
	// the mismatch handling lives at the call site, not in applyDelta.
	current := Snapshot{
		Revision: 5,
		Values:   map[string]any{"a": 1.0},
	}
	d := Delta{Revision: 99, From: 12345, Values: FieldDelta{Set: map[string]any{"a": 2.0}}}

	result := applyDelta(current, d)

	if result.Revision != 99 || result.Values["a"] != 2.0 {
		t.Errorf("applyDelta result = %+v, want it to apply mechanically regardless of From", result)
	}
}

// deltaVectors mirrors the shape documented by delta-vectors.json's
// "_comment": {base: Snapshot, steps: [{delta: Delta, expected: Snapshot}, ...]}.
// json.Unmarshal ignores the unknown "_comment" key by default — this must
// NOT use DisallowUnknownFields.
type deltaVectors struct {
	Base  Snapshot `json:"base"`
	Steps []struct {
		Delta    Delta    `json:"delta"`
		Expected Snapshot `json:"expected"`
	} `json:"steps"`
}

func TestApplyDeltaMatchesGoldenVectors(t *testing.T) {
	data, err := os.ReadFile("testdata/delta-vectors.json")
	if err != nil {
		t.Fatalf("reading testdata/delta-vectors.json: %v", err)
	}

	var vectors deltaVectors
	if err := json.Unmarshal(data, &vectors); err != nil {
		t.Fatalf("unmarshalling delta-vectors.json: %v", err)
	}
	if len(vectors.Steps) == 0 {
		t.Fatal("delta-vectors.json has no steps")
	}

	current := vectors.Base
	for i, step := range vectors.Steps {
		current = applyDelta(current, step.Delta)

		if !reflect.DeepEqual(current.Values, step.Expected.Values) {
			t.Errorf("step %d: Values = %#v, want %#v", i, current.Values, step.Expected.Values)
		}
		if !reflect.DeepEqual(current.Targeting, step.Expected.Targeting) {
			t.Errorf("step %d: Targeting = %#v, want %#v", i, current.Targeting, step.Expected.Targeting)
		}
		if current.Version != step.Expected.Version {
			t.Errorf("step %d: Version = %d, want %d", i, current.Version, step.Expected.Version)
		}
		if current.Revision != step.Expected.Revision {
			t.Errorf("step %d: Revision = %d, want %d", i, current.Revision, step.Expected.Revision)
		}
		if current.SchemaHash != step.Expected.SchemaHash {
			t.Errorf("step %d: SchemaHash = %q, want %q", i, current.SchemaHash, step.Expected.SchemaHash)
		}
	}
}
