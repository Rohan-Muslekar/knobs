package delivery

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/Rohan-Muslekar/knobs/internal/targeting"
)

func TestComputeDelta_ChangedValue(t *testing.T) {
	old := Snapshot{Revision: 1, Values: map[string]any{"retries": float64(3)}}
	new := Snapshot{Version: 2, Revision: 2, SchemaHash: "h2", Values: map[string]any{"retries": float64(5)}}

	d := ComputeDelta(old, new)

	if d.Type != "delta" {
		t.Fatalf("Type = %q, want %q", d.Type, "delta")
	}
	if d.Version != 2 || d.Revision != 2 || d.From != 1 || d.SchemaHash != "h2" {
		t.Fatalf("Delta header = %+v, want Version=2 Revision=2 From=1 SchemaHash=h2", d)
	}
	if len(d.Values.Set) != 1 || d.Values.Set["retries"] != float64(5) {
		t.Fatalf("Values.Set = %v, want {retries: 5}", d.Values.Set)
	}
	if len(d.Values.Remove) != 0 {
		t.Fatalf("Values.Remove = %v, want empty", d.Values.Remove)
	}
}

func TestComputeDelta_AddedValue(t *testing.T) {
	old := Snapshot{Revision: 1, Values: map[string]any{"retries": float64(3)}}
	new := Snapshot{Revision: 2, Values: map[string]any{"retries": float64(3), "timeout": float64(30)}}

	d := ComputeDelta(old, new)

	if len(d.Values.Set) != 1 || d.Values.Set["timeout"] != float64(30) {
		t.Fatalf("Values.Set = %v, want {timeout: 30}", d.Values.Set)
	}
	if _, ok := d.Values.Set["retries"]; ok {
		t.Fatalf("Values.Set unexpectedly includes unchanged key retries: %v", d.Values.Set)
	}
	if len(d.Values.Remove) != 0 {
		t.Fatalf("Values.Remove = %v, want empty", d.Values.Remove)
	}
}

func TestComputeDelta_RemovedValue(t *testing.T) {
	old := Snapshot{Revision: 1, Values: map[string]any{"retries": float64(3), "timeout": float64(30)}}
	new := Snapshot{Revision: 2, Values: map[string]any{"retries": float64(3)}}

	d := ComputeDelta(old, new)

	if len(d.Values.Set) != 0 {
		t.Fatalf("Values.Set = %v, want empty", d.Values.Set)
	}
	if !reflect.DeepEqual(d.Values.Remove, []string{"timeout"}) {
		t.Fatalf("Values.Remove = %v, want [timeout]", d.Values.Remove)
	}
}

func TestComputeDelta_RemovedValueSortsMultipleKeys(t *testing.T) {
	old := Snapshot{Revision: 1, Values: map[string]any{"z": 1, "a": 2, "m": 3}}
	new := Snapshot{Revision: 2, Values: map[string]any{}}

	d := ComputeDelta(old, new)

	if !reflect.DeepEqual(d.Values.Remove, []string{"a", "m", "z"}) {
		t.Fatalf("Values.Remove = %v, want sorted [a m z]", d.Values.Remove)
	}
}

func TestComputeDelta_ChangedTargetingRule(t *testing.T) {
	old := Snapshot{Revision: 1, Targeting: targeting.Map{
		"beta": {{Value: false}},
	}}
	new := Snapshot{Revision: 2, Targeting: targeting.Map{
		"beta": {{Value: true}},
	}}

	d := ComputeDelta(old, new)

	if len(d.Targeting.Set) != 1 {
		t.Fatalf("Targeting.Set = %v, want 1 entry", d.Targeting.Set)
	}
	got, ok := d.Targeting.Set["beta"]
	if !ok || len(got) != 1 || got[0].Value != true {
		t.Fatalf("Targeting.Set[beta] = %v, want [{Value: true}]", got)
	}
	if len(d.Targeting.Remove) != 0 {
		t.Fatalf("Targeting.Remove = %v, want empty", d.Targeting.Remove)
	}
}

func TestComputeDelta_RemovedTargetingKey(t *testing.T) {
	old := Snapshot{Revision: 1, Targeting: targeting.Map{
		"beta":  {{Value: true}},
		"gamma": {{Value: false}},
	}}
	new := Snapshot{Revision: 2, Targeting: targeting.Map{
		"beta": {{Value: true}},
	}}

	d := ComputeDelta(old, new)

	if len(d.Targeting.Set) != 0 {
		t.Fatalf("Targeting.Set = %v, want empty", d.Targeting.Set)
	}
	if !reflect.DeepEqual(d.Targeting.Remove, []string{"gamma"}) {
		t.Fatalf("Targeting.Remove = %v, want [gamma]", d.Targeting.Remove)
	}
}

func TestComputeDelta_NoChange(t *testing.T) {
	snap := Snapshot{
		Version: 1, Revision: 1, SchemaHash: "h1",
		Values:    map[string]any{"retries": float64(3)},
		Targeting: targeting.Map{"beta": {{Value: true}}},
	}

	d := ComputeDelta(snap, snap)

	if len(d.Values.Set) != 0 || len(d.Values.Remove) != 0 {
		t.Fatalf("Values delta = %+v, want empty Set and Remove", d.Values)
	}
	if len(d.Targeting.Set) != 0 || len(d.Targeting.Remove) != 0 {
		t.Fatalf("Targeting delta = %+v, want empty Set and Remove", d.Targeting)
	}
	if d.From != snap.Revision {
		t.Fatalf("From = %d, want %d", d.From, snap.Revision)
	}
}

func TestComputeDelta_DoesNotMutateInputs(t *testing.T) {
	old := Snapshot{
		Revision:  1,
		Values:    map[string]any{"retries": float64(3), "gone": float64(1)},
		Targeting: targeting.Map{"beta": {{Value: false}}, "goneKey": {{Value: true}}},
	}
	new := Snapshot{
		Revision:  2,
		Values:    map[string]any{"retries": float64(5)},
		Targeting: targeting.Map{"beta": {{Value: true}}},
	}
	oldCopy := cloneSnapshotForTest(old)
	newCopy := cloneSnapshotForTest(new)

	ComputeDelta(old, new)

	if !reflect.DeepEqual(old, oldCopy) {
		t.Fatalf("ComputeDelta mutated old: got %+v, want %+v", old, oldCopy)
	}
	if !reflect.DeepEqual(new, newCopy) {
		t.Fatalf("ComputeDelta mutated new: got %+v, want %+v", new, newCopy)
	}
}

func TestApplyDelta_RevisionMismatch(t *testing.T) {
	base := Snapshot{Revision: 5, Values: map[string]any{"retries": float64(3)}}
	d := Delta{Type: "delta", From: 4, Revision: 6}

	got, ok := ApplyDelta(base, d)

	if ok {
		t.Fatal("ApplyDelta returned ok=true for mismatched From")
	}
	if !reflect.DeepEqual(got, base) {
		t.Fatalf("ApplyDelta = %+v, want unchanged base %+v", got, base)
	}
}

func TestApplyDelta_DoesNotMutateBase(t *testing.T) {
	base := Snapshot{
		Revision:  1,
		Values:    map[string]any{"retries": float64(3)},
		Targeting: targeting.Map{"beta": {{Value: false}}},
	}
	baseCopy := cloneSnapshotForTest(base)
	d := ComputeDelta(base, Snapshot{
		Revision:  2,
		Values:    map[string]any{"retries": float64(9), "timeout": float64(1)},
		Targeting: targeting.Map{"beta": {{Value: true}}, "gamma": {{Value: true}}},
	})

	result, ok := ApplyDelta(base, d)
	if !ok {
		t.Fatal("ApplyDelta returned ok=false, want true")
	}
	if !reflect.DeepEqual(base, baseCopy) {
		t.Fatalf("ApplyDelta mutated base: got %+v, want %+v", base, baseCopy)
	}
	// Mutating the result's maps must not reach back into base's maps.
	result.Values["retries"] = float64(999)
	if base.Values["retries"] != float64(3) {
		t.Fatalf("base.Values aliased with result.Values: base.Values[retries] = %v", base.Values["retries"])
	}
}

func TestApplyDelta_RoundTrip(t *testing.T) {
	cases := []struct {
		name string
		base Snapshot
		new  Snapshot
	}{
		{
			name: "changed and added value",
			base: Snapshot{Version: 1, Revision: 1, SchemaHash: "h1", Values: map[string]any{"retries": float64(3)}},
			new:  Snapshot{Version: 1, Revision: 2, SchemaHash: "h1", Values: map[string]any{"retries": float64(5), "timeout": float64(30)}},
		},
		{
			name: "removed value",
			base: Snapshot{Version: 1, Revision: 1, SchemaHash: "h1", Values: map[string]any{"retries": float64(3), "timeout": float64(30)}},
			new:  Snapshot{Version: 1, Revision: 2, SchemaHash: "h1", Values: map[string]any{"retries": float64(3)}},
		},
		{
			name: "targeting changed and removed",
			base: Snapshot{Version: 1, Revision: 1, SchemaHash: "h1",
				Values:    map[string]any{"retries": float64(3)},
				Targeting: targeting.Map{"beta": {{Value: false}}, "gamma": {{Value: true}}},
			},
			new: Snapshot{Version: 2, Revision: 2, SchemaHash: "h2",
				Values:    map[string]any{"retries": float64(3)},
				Targeting: targeting.Map{"beta": {{Value: true}}},
			},
		},
		{
			name: "targeting added from nil",
			base: Snapshot{Version: 1, Revision: 1, SchemaHash: "h1", Values: map[string]any{}},
			new: Snapshot{Version: 1, Revision: 2, SchemaHash: "h1", Values: map[string]any{},
				Targeting: targeting.Map{"beta": {{Value: true}}},
			},
		},
		{
			name: "targeting cleared to nil",
			base: Snapshot{Version: 1, Revision: 1, SchemaHash: "h1", Values: map[string]any{},
				Targeting: targeting.Map{"beta": {{Value: true}}},
			},
			new: Snapshot{Version: 1, Revision: 2, SchemaHash: "h1", Values: map[string]any{}},
		},
		{
			name: "no change",
			base: Snapshot{Version: 1, Revision: 1, SchemaHash: "h1", Values: map[string]any{"retries": float64(3)}},
			new:  Snapshot{Version: 1, Revision: 1, SchemaHash: "h1", Values: map[string]any{"retries": float64(3)}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := ComputeDelta(tc.base, tc.new)
			got, ok := ApplyDelta(tc.base, d)
			if !ok {
				t.Fatalf("ApplyDelta returned ok=false, want true")
			}
			if !reflect.DeepEqual(got.Values, tc.new.Values) {
				t.Fatalf("Values = %v, want %v", got.Values, tc.new.Values)
			}
			if !reflect.DeepEqual(got.Targeting, tc.new.Targeting) {
				t.Fatalf("Targeting = %v, want %v", got.Targeting, tc.new.Targeting)
			}
			if got.Version != tc.new.Version || got.Revision != tc.new.Revision || got.SchemaHash != tc.new.SchemaHash {
				t.Fatalf("header = {%d %d %q}, want {%d %d %q}", got.Version, got.Revision, got.SchemaHash, tc.new.Version, tc.new.Revision, tc.new.SchemaHash)
			}
		})
	}
}

func TestFullFrameJSON(t *testing.T) {
	s := Snapshot{
		Version: 3, Revision: 7, SchemaHash: "h3",
		Values:    map[string]any{"retries": float64(3)},
		Targeting: targeting.Map{"beta": {{Value: true}}},
	}

	raw, err := FullFrameJSON(s)
	if err != nil {
		t.Fatalf("FullFrameJSON: %v", err)
	}
	if !strings.Contains(string(raw), `"type":"snapshot"`) {
		t.Fatalf("FullFrameJSON output missing type:snapshot: %s", raw)
	}

	var round Snapshot
	if err := json.Unmarshal(raw, &round); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if round.Version != s.Version || round.Revision != s.Revision || round.SchemaHash != s.SchemaHash {
		t.Fatalf("round-tripped header = %+v, want %+v", round, s)
	}
	if !reflect.DeepEqual(round.Values, s.Values) {
		t.Fatalf("round-tripped Values = %v, want %v", round.Values, s.Values)
	}
	if !reflect.DeepEqual(round.Targeting, s.Targeting) {
		t.Fatalf("round-tripped Targeting = %v, want %v", round.Targeting, s.Targeting)
	}

	// A bare snapshot marshal must still produce every key FullFrameJSON
	// does, other than "type" — the non-delta path is untouched.
	bare, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal(s): %v", err)
	}
	var bareFields, fullFields map[string]json.RawMessage
	if err := json.Unmarshal(bare, &bareFields); err != nil {
		t.Fatalf("Unmarshal(bare): %v", err)
	}
	if err := json.Unmarshal(raw, &fullFields); err != nil {
		t.Fatalf("Unmarshal(raw): %v", err)
	}
	delete(fullFields, "type")
	if !reflect.DeepEqual(bareFields, fullFields) {
		t.Fatalf("FullFrameJSON fields (minus type) = %v, want %v", fullFields, bareFields)
	}
}

// cloneSnapshotForTest deep-copies a Snapshot's maps so a test can assert
// the original wasn't mutated after passing it to a function under test.
func cloneSnapshotForTest(s Snapshot) Snapshot {
	clone := s
	if s.Values != nil {
		clone.Values = make(map[string]any, len(s.Values))
		for k, v := range s.Values {
			clone.Values[k] = v
		}
	}
	if s.Targeting != nil {
		clone.Targeting = make(targeting.Map, len(s.Targeting))
		for k, v := range s.Targeting {
			rules := make([]targeting.Rule, len(v))
			copy(rules, v)
			clone.Targeting[k] = rules
		}
	}
	return clone
}

// deltaVectorsFile mirrors internal/delivery/testdata/delta-vectors.json:
// a base snapshot plus a sequence of {delta, expected} steps applied one
// after another. SDKs reuse this file byte-identical to verify their own
// ApplyDelta against the reference implementation here.
type deltaVectorsFile struct {
	Base  Snapshot `json:"base"`
	Steps []struct {
		Delta    Delta    `json:"delta"`
		Expected Snapshot `json:"expected"`
	} `json:"steps"`
}

func TestApplyDelta_AgainstVectorsFile(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "delta-vectors.json"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var vectors deltaVectorsFile
	if err := json.Unmarshal(raw, &vectors); err != nil {
		t.Fatalf("Unmarshal vectors: %v", err)
	}
	if len(vectors.Steps) == 0 {
		t.Fatal("vectors file has no steps")
	}

	current := vectors.Base
	for i, step := range vectors.Steps {
		got, ok := ApplyDelta(current, step.Delta)
		if !ok {
			t.Fatalf("step %d: ApplyDelta returned ok=false", i)
		}
		if got.Version != step.Expected.Version || got.Revision != step.Expected.Revision || got.SchemaHash != step.Expected.SchemaHash {
			t.Fatalf("step %d: header = %+v, want %+v", i, got, step.Expected)
		}
		if !reflect.DeepEqual(got.Values, step.Expected.Values) {
			t.Fatalf("step %d: Values = %v, want %v", i, got.Values, step.Expected.Values)
		}
		if !reflect.DeepEqual(got.Targeting, step.Expected.Targeting) {
			t.Fatalf("step %d: Targeting = %v, want %v", i, got.Targeting, step.Expected.Targeting)
		}
		current = got
	}
}

// Sanity check the test helper itself sorts as expected, matching the
// determinism ComputeDelta promises for Remove slices.
func TestComputeDelta_RemoveIsSorted(t *testing.T) {
	old := Snapshot{Revision: 1, Values: map[string]any{"c": 1, "a": 1, "b": 1}}
	new := Snapshot{Revision: 2, Values: map[string]any{}}
	d := ComputeDelta(old, new)
	sorted := append([]string{}, d.Values.Remove...)
	sort.Strings(sorted)
	if !reflect.DeepEqual(d.Values.Remove, sorted) {
		t.Fatalf("Values.Remove = %v is not sorted", d.Values.Remove)
	}
}
