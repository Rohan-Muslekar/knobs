//go:build integration

package api_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestValuesAPI(t *testing.T) {
	router, cookie, repo := seededRouter(t)

	projRec := post(router, "/v1/projects", `{"name":"Acme","slug":"acme"}`, cookie)
	if projRec.Code != http.StatusCreated {
		t.Fatalf("create project = %d, want 201", projRec.Code)
	}
	var proj map[string]any
	_ = json.NewDecoder(projRec.Body).Decode(&proj)
	projectID, _ := proj["id"].(string)
	if projectID == "" {
		t.Fatal("no project id returned")
	}

	schemaBody := `{"fields":[{"name":"maxRetries","type":"int","required":true,"max":5}]}`
	if rec := put(router, "/v1/projects/"+projectID+"/schema", schemaBody, cookie); rec.Code != http.StatusOK {
		t.Fatalf("put schema = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	envRec := post(router, "/v1/projects/"+projectID+"/environments", `{"name":"staging"}`, cookie)
	if envRec.Code != http.StatusCreated {
		t.Fatalf("create env = %d, want 201, body=%s", envRec.Code, envRec.Body.String())
	}
	var env map[string]any
	_ = json.NewDecoder(envRec.Body).Decode(&env)
	envID, _ := env["id"].(string)
	if envID == "" {
		t.Fatal("no environment id returned")
	}

	// No values yet: 404 "no values set".
	if rec := get(router, "/v1/environments/"+envID+"/values", cookie); rec.Code != http.StatusNotFound {
		t.Fatalf("get values (none yet) = %d, want 404", rec.Code)
	}

	// Invalid values (violates max:5) are rejected before any version is created.
	invalidBody := `{"values":{"maxRetries":9}}`
	if rec := put(router, "/v1/environments/"+envID+"/values", invalidBody, cookie); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("put invalid values = %d, want 422, body=%s", rec.Code, rec.Body.String())
	}

	// Valid values create version 1 and make it current.
	validBody := `{"values":{"maxRetries":3}}`
	putRec := put(router, "/v1/environments/"+envID+"/values", validBody, cookie)
	if putRec.Code != http.StatusOK {
		t.Fatalf("put valid values = %d, want 200, body=%s", putRec.Code, putRec.Body.String())
	}
	var v1 map[string]any
	_ = json.NewDecoder(putRec.Body).Decode(&v1)
	if v, _ := v1["version"].(float64); v != 1 {
		t.Fatalf("put values version = %v, want 1", v1["version"])
	}

	getRec := get(router, "/v1/environments/"+envID+"/values", cookie)
	if getRec.Code != http.StatusOK {
		t.Fatalf("get values = %d, want 200", getRec.Code)
	}
	var got1 map[string]any
	_ = json.NewDecoder(getRec.Body).Decode(&got1)
	if v, _ := got1["version"].(float64); v != 1 {
		t.Fatalf("get values version = %v, want 1", got1["version"])
	}
	values1, _ := got1["values"].(map[string]any)
	if mr, _ := values1["maxRetries"].(float64); mr != 3 {
		t.Fatalf("get values maxRetries = %v, want 3", values1["maxRetries"])
	}

	// Putting again creates a new, immutable version 2.
	putRec2 := put(router, "/v1/environments/"+envID+"/values", validBody, cookie)
	if putRec2.Code != http.StatusOK {
		t.Fatalf("put values (2nd) = %d, want 200, body=%s", putRec2.Code, putRec2.Body.String())
	}
	var v2 map[string]any
	_ = json.NewDecoder(putRec2.Body).Decode(&v2)
	if v, _ := v2["version"].(float64); v != 2 {
		t.Fatalf("put values (2nd) version = %v, want 2", v2["version"])
	}

	getRec2 := get(router, "/v1/environments/"+envID+"/values", cookie)
	if getRec2.Code != http.StatusOK {
		t.Fatalf("get values (2nd) = %d, want 200", getRec2.Code)
	}
	var got2 map[string]any
	_ = json.NewDecoder(getRec2.Body).Decode(&got2)
	if v, _ := got2["version"].(float64); v != 2 {
		t.Fatalf("get values (2nd) version = %v, want 2", got2["version"])
	}

	// Unauthenticated access is rejected.
	if rec := get(router, "/v1/environments/"+envID+"/values", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth get values = %d, want 401", rec.Code)
	}
	if rec := put(router, "/v1/environments/"+envID+"/values", validBody, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth put values = %d, want 401", rec.Code)
	}

	// Audit read-back: both successful PUTs must have recorded values.update
	// in the same tx as the write that produced them.
	rows, err := repo.Pool().Query(t.Context(),
		`SELECT action FROM audit_log WHERE target = $1 ORDER BY at`, envID)
	if err != nil {
		t.Fatalf("audit query: %v", err)
	}
	var actions []string
	for rows.Next() {
		var action string
		if err := rows.Scan(&action); err != nil {
			t.Fatalf("audit scan: %v", err)
		}
		actions = append(actions, action)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("audit rows: %v", err)
	}
	updateCount := 0
	for _, a := range actions {
		if a == "values.update" {
			updateCount++
		}
	}
	if updateCount != 2 {
		t.Fatalf("audit_log values.update count = %d, want 2, actions=%v", updateCount, actions)
	}

	// Change-safety close-out (Task 6's deferred case): with maxRetries:3
	// live as the environment's current version, tightening the schema's
	// max below that live value must be rejected with 409 instead of
	// silently breaking the environment's current config.
	tighterSchema := `{"fields":[{"name":"maxRetries","type":"int","required":true,"max":2}]}`
	conflictRec := put(router, "/v1/projects/"+projectID+"/schema", tighterSchema, cookie)
	if conflictRec.Code != http.StatusConflict {
		t.Fatalf("put tighter schema against live value 3 = %d, want 409, body=%s", conflictRec.Code, conflictRec.Body.String())
	}
}

// TestValuesAPI_Targeting exercises targeting end to end: a PUT carrying
// targeting rules must validate them (rejecting a rule against an unknown
// field with 422), persist them alongside the values, surface them from
// GET /v1/values, ride along in /v1/snapshot, and — on rollback — restore
// whichever version's targeting the rollback lands on rather than leaking
// the targeting from the version that was current before the rollback.
func TestValuesAPI_Targeting(t *testing.T) {
	router, cookie, _ := seededRouter(t)

	projRec := post(router, "/v1/projects", `{"name":"Acme","slug":"acme"}`, cookie)
	if projRec.Code != http.StatusCreated {
		t.Fatalf("create project = %d, want 201, body=%s", projRec.Code, projRec.Body.String())
	}
	var proj map[string]any
	_ = json.NewDecoder(projRec.Body).Decode(&proj)
	projectID, _ := proj["id"].(string)

	schemaBody := `{"fields":[{"name":"maxRetries","type":"int","required":true,"max":10}]}`
	if rec := put(router, "/v1/projects/"+projectID+"/schema", schemaBody, cookie); rec.Code != http.StatusOK {
		t.Fatalf("put schema = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	envRec := post(router, "/v1/projects/"+projectID+"/environments", `{"name":"staging"}`, cookie)
	if envRec.Code != http.StatusCreated {
		t.Fatalf("create env = %d, want 201, body=%s", envRec.Code, envRec.Body.String())
	}
	var env map[string]any
	_ = json.NewDecoder(envRec.Body).Decode(&env)
	envID, _ := env["id"].(string)

	// Targeting against a field that doesn't exist in the schema is
	// rejected with 422, before any version is created — same treatment
	// as a bad value.
	badTargetingBody := `{"values":{"maxRetries":3},"targeting":{"noSuchField":[{"value":1}]}}`
	if rec := put(router, "/v1/environments/"+envID+"/values", badTargetingBody, cookie); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("put bad targeting = %d, want 422, body=%s", rec.Code, rec.Body.String())
	}

	// Version 1: values + targeting rules.
	v1Body := `{"values":{"maxRetries":3},"targeting":{"maxRetries":[{"value":5}]}}`
	putRec1 := put(router, "/v1/environments/"+envID+"/values", v1Body, cookie)
	if putRec1.Code != http.StatusOK {
		t.Fatalf("put v1 = %d, want 200, body=%s", putRec1.Code, putRec1.Body.String())
	}
	var v1 map[string]any
	_ = json.NewDecoder(putRec1.Body).Decode(&v1)
	if v, _ := v1["version"].(float64); v != 1 {
		t.Fatalf("put v1 version = %v, want 1", v1["version"])
	}
	assertTargetingHasMaxRetriesValue(t, v1["targeting"], 5)

	// GET values returns the same targeting.
	getRec1 := get(router, "/v1/environments/"+envID+"/values", cookie)
	if getRec1.Code != http.StatusOK {
		t.Fatalf("get v1 = %d, want 200, body=%s", getRec1.Code, getRec1.Body.String())
	}
	var got1 map[string]any
	_ = json.NewDecoder(getRec1.Body).Decode(&got1)
	assertTargetingHasMaxRetriesValue(t, got1["targeting"], 5)

	// /v1/snapshot (delivery surface, API-key auth) includes targeting too.
	keyRec := post(router, "/v1/environments/"+envID+"/api-keys", `{"name":"sdk"}`, cookie)
	if keyRec.Code != http.StatusCreated {
		t.Fatalf("create key = %d, want 201, body=%s", keyRec.Code, keyRec.Body.String())
	}
	var keyBody map[string]any
	_ = json.NewDecoder(keyRec.Body).Decode(&keyBody)
	key, _ := keyBody["key"].(string)
	if key == "" {
		t.Fatal("no plaintext key returned")
	}

	snapRec1 := bearer(router, "/v1/snapshot", key)
	if snapRec1.Code != http.StatusOK {
		t.Fatalf("snapshot v1 = %d, want 200, body=%s", snapRec1.Code, snapRec1.Body.String())
	}
	var snap1 map[string]any
	_ = json.NewDecoder(snapRec1.Body).Decode(&snap1)
	assertTargetingHasMaxRetriesValue(t, snap1["targeting"], 5)

	// Version 2: different values, different targeting.
	v2Body := `{"values":{"maxRetries":4},"targeting":{"maxRetries":[{"value":9}]}}`
	putRec2 := put(router, "/v1/environments/"+envID+"/values", v2Body, cookie)
	if putRec2.Code != http.StatusOK {
		t.Fatalf("put v2 = %d, want 200, body=%s", putRec2.Code, putRec2.Body.String())
	}
	var v2 map[string]any
	_ = json.NewDecoder(putRec2.Body).Decode(&v2)
	if v, _ := v2["version"].(float64); v != 2 {
		t.Fatalf("put v2 version = %v, want 2", v2["version"])
	}
	assertTargetingHasMaxRetriesValue(t, v2["targeting"], 9)

	snapRec2 := bearer(router, "/v1/snapshot", key)
	if snapRec2.Code != http.StatusOK {
		t.Fatalf("snapshot v2 = %d, want 200, body=%s", snapRec2.Code, snapRec2.Body.String())
	}
	var snap2 map[string]any
	_ = json.NewDecoder(snapRec2.Body).Decode(&snap2)
	assertTargetingHasMaxRetriesValue(t, snap2["targeting"], 9)

	// Rollback to version 1 must restore v1's targeting, not v2's.
	rollbackRec := post(router, "/v1/environments/"+envID+"/rollback", `{"version":1}`, cookie)
	if rollbackRec.Code != http.StatusOK {
		t.Fatalf("rollback = %d, want 200, body=%s", rollbackRec.Code, rollbackRec.Body.String())
	}
	var rollbackBody map[string]any
	_ = json.NewDecoder(rollbackRec.Body).Decode(&rollbackBody)
	if v, _ := rollbackBody["version"].(float64); v != 1 {
		t.Fatalf("rollback response version = %v, want 1", rollbackBody["version"])
	}
	assertTargetingHasMaxRetriesValue(t, rollbackBody["targeting"], 5)

	getRec3 := get(router, "/v1/environments/"+envID+"/values", cookie)
	if getRec3.Code != http.StatusOK {
		t.Fatalf("get after rollback = %d, want 200, body=%s", getRec3.Code, getRec3.Body.String())
	}
	var got3 map[string]any
	_ = json.NewDecoder(getRec3.Body).Decode(&got3)
	assertTargetingHasMaxRetriesValue(t, got3["targeting"], 5)

	snapRec3 := bearer(router, "/v1/snapshot", key)
	if snapRec3.Code != http.StatusOK {
		t.Fatalf("snapshot after rollback = %d, want 200, body=%s", snapRec3.Code, snapRec3.Body.String())
	}
	var snap3 map[string]any
	_ = json.NewDecoder(snapRec3.Body).Decode(&snap3)
	assertTargetingHasMaxRetriesValue(t, snap3["targeting"], 5)
}

// assertTargetingHasMaxRetriesValue checks that raw decodes as
// {"maxRetries": [{"value": want, ...}, ...]} — the shape targeting.Map
// takes over the wire.
func assertTargetingHasMaxRetriesValue(t *testing.T, raw any, want float64) {
	t.Helper()
	m, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("targeting = %v (%T), want a map", raw, raw)
	}
	rules, ok := m["maxRetries"].([]any)
	if !ok || len(rules) == 0 {
		t.Fatalf("targeting[maxRetries] = %v, want a non-empty rule list", m["maxRetries"])
	}
	rule, ok := rules[0].(map[string]any)
	if !ok {
		t.Fatalf("targeting[maxRetries][0] = %v, want a rule object", rules[0])
	}
	if got, _ := rule["value"].(float64); got != want {
		t.Fatalf("targeting[maxRetries][0].value = %v, want %v", rule["value"], want)
	}
}
