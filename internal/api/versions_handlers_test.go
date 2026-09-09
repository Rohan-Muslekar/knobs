//go:build integration

package api_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestVersionsAPI(t *testing.T) {
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
	if envID == "" {
		t.Fatal("no environment id returned")
	}

	// Two PUTs produce version 1 and version 2.
	if rec := put(router, "/v1/environments/"+envID+"/values", `{"values":{"maxRetries":3}}`, cookie); rec.Code != http.StatusOK {
		t.Fatalf("put values v1 = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if rec := put(router, "/v1/environments/"+envID+"/values", `{"values":{"maxRetries":4}}`, cookie); rec.Code != http.StatusOK {
		t.Fatalf("put values v2 = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	// GET versions: 2 entries, v2 is current.
	listRec := get(router, "/v1/environments/"+envID+"/versions", cookie)
	if listRec.Code != http.StatusOK {
		t.Fatalf("get versions = %d, want 200, body=%s", listRec.Code, listRec.Body.String())
	}
	var versions []map[string]any
	_ = json.NewDecoder(listRec.Body).Decode(&versions)
	if len(versions) != 2 {
		t.Fatalf("versions len = %d, want 2, body=%v", len(versions), versions)
	}
	byVersion := map[float64]map[string]any{}
	for _, v := range versions {
		n, _ := v["version"].(float64)
		byVersion[n] = v
	}
	if cur, _ := byVersion[2]["isCurrent"].(bool); !cur {
		t.Fatalf("version 2 isCurrent = %v, want true, versions=%v", byVersion[2]["isCurrent"], versions)
	}
	if cur, _ := byVersion[1]["isCurrent"].(bool); cur {
		t.Fatalf("version 1 isCurrent = %v, want false, versions=%v", byVersion[1]["isCurrent"], versions)
	}

	// Rollback to version 1.
	rollbackRec := post(router, "/v1/environments/"+envID+"/rollback", `{"version":1}`, cookie)
	if rollbackRec.Code != http.StatusOK {
		t.Fatalf("rollback = %d, want 200, body=%s", rollbackRec.Code, rollbackRec.Body.String())
	}
	var rollbackBody map[string]any
	_ = json.NewDecoder(rollbackRec.Body).Decode(&rollbackBody)
	if v, _ := rollbackBody["version"].(float64); v != 1 {
		t.Fatalf("rollback response version = %v, want 1", rollbackBody["version"])
	}

	// GET values now returns v1's values.
	valuesRec := get(router, "/v1/environments/"+envID+"/values", cookie)
	if valuesRec.Code != http.StatusOK {
		t.Fatalf("get values after rollback = %d, want 200", valuesRec.Code)
	}
	var valuesBody map[string]any
	_ = json.NewDecoder(valuesRec.Body).Decode(&valuesBody)
	if v, _ := valuesBody["version"].(float64); v != 1 {
		t.Fatalf("values version after rollback = %v, want 1", valuesBody["version"])
	}
	values, _ := valuesBody["values"].(map[string]any)
	if mr, _ := values["maxRetries"].(float64); mr != 3 {
		t.Fatalf("values maxRetries after rollback = %v, want 3", values["maxRetries"])
	}

	// GET versions again: v1 is now current.
	listRec2 := get(router, "/v1/environments/"+envID+"/versions", cookie)
	if listRec2.Code != http.StatusOK {
		t.Fatalf("get versions (2nd) = %d, want 200", listRec2.Code)
	}
	var versions2 []map[string]any
	_ = json.NewDecoder(listRec2.Body).Decode(&versions2)
	byVersion2 := map[float64]map[string]any{}
	for _, v := range versions2 {
		n, _ := v["version"].(float64)
		byVersion2[n] = v
	}
	if cur, _ := byVersion2[1]["isCurrent"].(bool); !cur {
		t.Fatalf("version 1 isCurrent (after rollback) = %v, want true, versions=%v", byVersion2[1]["isCurrent"], versions2)
	}
	if cur, _ := byVersion2[2]["isCurrent"].(bool); cur {
		t.Fatalf("version 2 isCurrent (after rollback) = %v, want false, versions=%v", byVersion2[2]["isCurrent"], versions2)
	}

	// Rollback to a nonexistent version is a 404.
	badRec := post(router, "/v1/environments/"+envID+"/rollback", `{"version":99}`, cookie)
	if badRec.Code != http.StatusNotFound {
		t.Fatalf("rollback to missing version = %d, want 404, body=%s", badRec.Code, badRec.Body.String())
	}

	// Rollback with a well-formed but missing version field is a 422.
	if rec := post(router, "/v1/environments/"+envID+"/rollback", `{}`, cookie); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("rollback missing version field = %d, want 422, body=%s", rec.Code, rec.Body.String())
	}
	// Rollback with an unparseable body is a 400.
	if rec := post(router, "/v1/environments/"+envID+"/rollback", `not json`, cookie); rec.Code != http.StatusBadRequest {
		t.Fatalf("rollback unparseable body = %d, want 400, body=%s", rec.Code, rec.Body.String())
	}

	// Unauthenticated access is rejected.
	if rec := get(router, "/v1/environments/"+envID+"/versions", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth get versions = %d, want 401", rec.Code)
	}
	if rec := post(router, "/v1/environments/"+envID+"/rollback", `{"version":1}`, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth rollback = %d, want 401", rec.Code)
	}

	// Audit read-back: the successful rollback above must have recorded
	// values.rollback in the same tx as the write that produced it.
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
	rollbackCount := 0
	for _, a := range actions {
		if a == "values.rollback" {
			rollbackCount++
		}
	}
	if rollbackCount != 1 {
		t.Fatalf("audit_log values.rollback count = %d, want 1, actions=%v", rollbackCount, actions)
	}
}
