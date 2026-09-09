//go:build integration

package api_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

func TestAuditAPI(t *testing.T) {
	router, cookie, _ := seededRouter(t)

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

	patchRec := patch(router, "/v1/projects/"+projectID, `{"name":"Acme Inc"}`, cookie)
	if patchRec.Code != http.StatusOK {
		t.Fatalf("patch project = %d, want 200, body=%s", patchRec.Code, patchRec.Body.String())
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

	if rec := put(router, "/v1/environments/"+envID+"/values", `{"values":{"maxRetries":3}}`, cookie); rec.Code != http.StatusOK {
		t.Fatalf("put values = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	// GET audit: at least 3 entries (project.create, project.rename,
	// values.update), newest first.
	listRec := get(router, "/v1/projects/"+projectID+"/audit", cookie)
	if listRec.Code != http.StatusOK {
		t.Fatalf("get audit = %d, want 200, body=%s", listRec.Code, listRec.Body.String())
	}
	var entries []map[string]any
	if err := json.NewDecoder(listRec.Body).Decode(&entries); err != nil {
		t.Fatalf("decode audit list: %v", err)
	}
	if len(entries) < 3 {
		t.Fatalf("audit entries len = %d, want >= 3, body=%v", len(entries), entries)
	}
	// The most recent action recorded above is the values.update, so it
	// must be first in a newest-first ordering.
	if action, _ := entries[0]["action"].(string); action != "values.update" {
		t.Fatalf("entries[0].action = %q, want %q (newest first), entries=%v", action, "values.update", entries)
	}
	for _, e := range entries {
		if _, ok := e["id"]; !ok {
			t.Fatalf("entry missing id field: %v", e)
		}
		if _, ok := e["at"]; !ok {
			t.Fatalf("entry missing at field: %v", e)
		}
	}

	// diff must be inlined as real nested JSON (an object with fields we can
	// index into), not a JSON string containing escaped JSON.
	diff, ok := entries[0]["diff"].(map[string]any)
	if !ok {
		t.Fatalf("entries[0].diff = %v (%T), want a nested object", entries[0]["diff"], entries[0]["diff"])
	}
	if _, ok := diff["version"]; !ok {
		t.Fatalf("entries[0].diff missing %q key: %v", "version", diff)
	}

	// limit=1 returns exactly one entry, the newest.
	limitedRec := get(router, "/v1/projects/"+projectID+"/audit?limit=1", cookie)
	if limitedRec.Code != http.StatusOK {
		t.Fatalf("get audit (limit=1) = %d, want 200", limitedRec.Code)
	}
	var limited []map[string]any
	_ = json.NewDecoder(limitedRec.Body).Decode(&limited)
	if len(limited) != 1 {
		t.Fatalf("audit entries (limit=1) len = %d, want 1, body=%v", len(limited), limited)
	}

	// Unauthenticated access is rejected.
	if rec := get(router, "/v1/projects/"+projectID+"/audit", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth get audit = %d, want 401", rec.Code)
	}

	// Missing project is a 404.
	if rec := get(router, "/v1/projects/"+uuid.New().String()+"/audit", cookie); rec.Code != http.StatusNotFound {
		t.Fatalf("get audit (missing project) = %d, want 404", rec.Code)
	}

	// An unparseable project id is a 400, not a 404.
	if rec := get(router, "/v1/projects/not-a-uuid/audit", cookie); rec.Code != http.StatusBadRequest {
		t.Fatalf("get audit (bad project id) = %d, want 400, body=%s", rec.Code, rec.Body.String())
	}
}
