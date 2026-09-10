//go:build integration

package api_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestSchemaAPI(t *testing.T) {
	router, cookie, repo, orgID := seededRouter(t)

	projRec := post(router, "/v1/projects", createProjectBody(orgID, "Acme", "acme"), cookie)
	if projRec.Code != http.StatusCreated {
		t.Fatalf("create project = %d, want 201", projRec.Code)
	}
	var proj map[string]any
	_ = json.NewDecoder(projRec.Body).Decode(&proj)
	projectID, _ := proj["id"].(string)
	if projectID == "" {
		t.Fatal("no project id returned")
	}

	// Fresh project: schema is auto-initialized to empty fields, version 1.
	getRec := get(router, "/v1/projects/"+projectID+"/schema", cookie)
	if getRec.Code != http.StatusOK {
		t.Fatalf("get schema = %d, want 200", getRec.Code)
	}
	var got map[string]any
	_ = json.NewDecoder(getRec.Body).Decode(&got)
	if v, _ := got["schemaVersion"].(float64); v != 1 {
		t.Fatalf("fresh schemaVersion = %v, want 1", got["schemaVersion"])
	}
	def, _ := got["definition"].(map[string]any)
	fields, _ := def["fields"].([]any)
	if len(fields) != 0 {
		t.Fatalf("fresh fields = %v, want empty", fields)
	}

	// PUT a valid field list.
	validBody := `{"fields":[{"name":"maxRetries","type":"int","required":true,"max":5},{"name":"featureX","type":"bool"}]}`
	putRec := put(router, "/v1/projects/"+projectID+"/schema", validBody, cookie)
	if putRec.Code != http.StatusOK {
		t.Fatalf("put valid schema = %d, want 200, body=%s", putRec.Code, putRec.Body.String())
	}
	var updated map[string]any
	_ = json.NewDecoder(putRec.Body).Decode(&updated)
	if v, _ := updated["schemaVersion"].(float64); v != 2 {
		t.Fatalf("updated schemaVersion = %v, want 2", updated["schemaVersion"])
	}

	// PUT an invalid definition (unknown type) is rejected before persisting.
	invalidBody := `{"fields":[{"name":"x","type":"widget"}]}`
	badRec := put(router, "/v1/projects/"+projectID+"/schema", invalidBody, cookie)
	if badRec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("put invalid schema = %d, want 422", badRec.Code)
	}

	// change-safety 409 covered in Task 7 once values/current-version rows exist

	// GET reflects the stored field list, unaffected by the rejected PUT.
	getRec2 := get(router, "/v1/projects/"+projectID+"/schema", cookie)
	if getRec2.Code != http.StatusOK {
		t.Fatalf("get schema (2) = %d, want 200", getRec2.Code)
	}
	var got2 map[string]any
	_ = json.NewDecoder(getRec2.Body).Decode(&got2)
	if v, _ := got2["schemaVersion"].(float64); v != 2 {
		t.Fatalf("2nd get schemaVersion = %v, want 2 (unchanged by rejected put)", got2["schemaVersion"])
	}
	def2, _ := got2["definition"].(map[string]any)
	fields2, _ := def2["fields"].([]any)
	if len(fields2) != 2 {
		t.Fatalf("2nd get fields = %v, want 2 entries", fields2)
	}
	first, _ := fields2[0].(map[string]any)
	if first["name"] != "maxRetries" || first["type"] != "int" {
		t.Fatalf("first field = %+v, want maxRetries/int", first)
	}

	// Unauthenticated access is rejected.
	if rec := get(router, "/v1/projects/"+projectID+"/schema", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth get schema = %d, want 401", rec.Code)
	}
	if rec := put(router, "/v1/projects/"+projectID+"/schema", validBody, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth put schema = %d, want 401", rec.Code)
	}

	// Audit read-back: the successful PUT must have recorded schema.update in
	// the same tx as the write that produced it.
	rows, err := repo.Pool().Query(t.Context(),
		`SELECT action FROM audit_log WHERE project_id = $1 ORDER BY at`, projectID)
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
	found := false
	for _, a := range actions {
		if a == "schema.update" {
			found = true
		}
	}
	if !found {
		t.Fatalf("audit_log missing action %q, got %v", "schema.update", actions)
	}
}
