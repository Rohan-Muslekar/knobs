//go:build integration

package api_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/Rohan-Muslekar/knobs/internal/api"
	"github.com/Rohan-Muslekar/knobs/internal/auth"
	"github.com/Rohan-Muslekar/knobs/internal/store"
)

// seededRouter returns a router, a valid session cookie for a seeded admin,
// and the repo backing it (so tests can read back state, e.g. audit_log,
// that the HTTP surface doesn't expose directly).
func seededRouter(t *testing.T) (http.Handler, string, *store.Repo) {
	t.Helper()
	repo := store.New(migratedPool(t))
	authr := auth.New("test-secret", false)
	hash, _ := authr.Hash("pw")
	if _, err := repo.CreateUser(t.Context(), repo.Pool(), "a@x.com", hash); err != nil {
		t.Fatalf("seed: %v", err)
	}
	router := api.NewRouter(api.Deps{Repo: repo, Auth: authr})
	rec := post(router, "/v1/auth/login", `{"email":"a@x.com","password":"pw"}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("login failed: %d", rec.Code)
	}
	return router, rec.Result().Cookies()[0].Value, repo
}

func TestProjectsAPI(t *testing.T) {
	router, cookie, repo := seededRouter(t)

	if rec := post(router, "/v1/projects", `{"name":"Acme","slug":"acme"}`, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth create = %d, want 401", rec.Code)
	}
	rec := post(router, "/v1/projects", `{"name":"Acme","slug":"acme"}`, cookie)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d, want 201", rec.Code)
	}
	var created map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&created)
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatal("no id returned")
	}

	if rec := post(router, "/v1/projects", `{"name":"Dup","slug":"acme"}`, cookie); rec.Code != http.StatusConflict {
		t.Fatalf("dup slug = %d, want 409", rec.Code)
	}
	if rec := post(router, "/v1/projects", `{"name":"Bad","slug":"Bad Slug"}`, cookie); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("bad slug = %d, want 422", rec.Code)
	}
	if rec := get(router, "/v1/projects/"+id, cookie); rec.Code != http.StatusOK {
		t.Fatalf("get = %d, want 200", rec.Code)
	}

	patchRec := patch(router, "/v1/projects/"+id, `{"name":"Acme Inc"}`, cookie)
	if patchRec.Code != http.StatusOK {
		t.Fatalf("patch = %d, want 200", patchRec.Code)
	}
	var patched map[string]any
	_ = json.NewDecoder(patchRec.Body).Decode(&patched)
	if name, _ := patched["name"].(string); name != "Acme Inc" {
		t.Fatalf("patch name = %q, want %q", name, "Acme Inc")
	}

	// Patch with an unparseable body is a 400.
	if rec := patch(router, "/v1/projects/"+id, `not json`, cookie); rec.Code != http.StatusBadRequest {
		t.Fatalf("patch unparseable body = %d, want 400, body=%s", rec.Code, rec.Body.String())
	}
	// Patch with a well-formed but missing name field is a 422.
	if rec := patch(router, "/v1/projects/"+id, `{}`, cookie); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("patch missing name field = %d, want 422, body=%s", rec.Code, rec.Body.String())
	}

	if rec := get(router, "/v1/projects/"+uuid.New().String(), cookie); rec.Code != http.StatusNotFound {
		t.Fatalf("get missing = %d, want 404", rec.Code)
	}

	listRec := get(router, "/v1/projects", cookie)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list = %d, want 200", listRec.Code)
	}
	var list []map[string]any
	_ = json.NewDecoder(listRec.Body).Decode(&list)
	if len(list) < 1 {
		t.Fatalf("list len = %d, want >= 1", len(list))
	}

	// Audit read-back: both the create and the rename must have landed in
	// audit_log for this project, in the same tx as the write that produced
	// them. A regression that swapped tx for Pool() in the handler would
	// leave this empty even though the HTTP responses above still pass.
	rows, err := repo.Pool().Query(t.Context(),
		`SELECT action FROM audit_log WHERE project_id = $1 ORDER BY at`, id)
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
	wantActions := map[string]bool{"project.create": false, "project.rename": false}
	for _, a := range actions {
		if _, ok := wantActions[a]; ok {
			wantActions[a] = true
		}
	}
	for action, seen := range wantActions {
		if !seen {
			t.Fatalf("audit_log missing action %q, got %v", action, actions)
		}
	}
}
