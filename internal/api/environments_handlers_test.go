//go:build integration

package api_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

func TestEnvironmentsAPI(t *testing.T) {
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

	if rec := post(router, "/v1/projects/"+projectID+"/environments", `{"name":"staging"}`, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth create = %d, want 401", rec.Code)
	}

	rec := post(router, "/v1/projects/"+projectID+"/environments", `{"name":"staging"}`, cookie)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create env = %d, want 201", rec.Code)
	}
	var created map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&created)
	envID, _ := created["id"].(string)
	if envID == "" {
		t.Fatal("no env id returned")
	}
	if created["currentVersionId"] != nil {
		t.Fatalf("currentVersionId = %v, want nil", created["currentVersionId"])
	}

	if rec := post(router, "/v1/projects/"+projectID+"/environments", `{"name":"staging"}`, cookie); rec.Code != http.StatusConflict {
		t.Fatalf("dup name = %d, want 409", rec.Code)
	}

	if rec := post(router, "/v1/projects/"+projectID+"/environments", `{"name":"Bad Name"}`, cookie); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("bad name = %d, want 422", rec.Code)
	}

	if rec := post(router, "/v1/projects/"+uuid.New().String()+"/environments", `{"name":"staging"}`, cookie); rec.Code != http.StatusNotFound {
		t.Fatalf("create under missing project = %d, want 404", rec.Code)
	}

	listRec := get(router, "/v1/projects/"+projectID+"/environments", cookie)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list = %d, want 200", listRec.Code)
	}
	var list []map[string]any
	_ = json.NewDecoder(listRec.Body).Decode(&list)
	if len(list) != 1 {
		t.Fatalf("list len = %d, want 1", len(list))
	}

	if rec := get(router, "/v1/projects/"+uuid.New().String()+"/environments", cookie); rec.Code != http.StatusNotFound {
		t.Fatalf("list under missing project = %d, want 404", rec.Code)
	}

	getRec := get(router, "/v1/environments/"+envID, cookie)
	if getRec.Code != http.StatusOK {
		t.Fatalf("get env = %d, want 200", getRec.Code)
	}
	var gotEnv map[string]any
	_ = json.NewDecoder(getRec.Body).Decode(&gotEnv)
	if name, _ := gotEnv["name"].(string); name != "staging" {
		t.Fatalf("get env name = %q, want %q", name, "staging")
	}

	if rec := get(router, "/v1/environments/"+uuid.New().String(), cookie); rec.Code != http.StatusNotFound {
		t.Fatalf("get missing env = %d, want 404", rec.Code)
	}

	if rec := get(router, "/v1/environments/"+envID, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth get = %d, want 401", rec.Code)
	}

	// Audit read-back: environment.create must have landed in audit_log in
	// the same tx as the insert that produced it.
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
		if a == "environment.create" {
			found = true
		}
	}
	if !found {
		t.Fatalf("audit_log missing action %q, got %v", "environment.create", actions)
	}
}
