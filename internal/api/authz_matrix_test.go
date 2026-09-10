//go:build integration

package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/Rohan-Muslekar/knobs/internal/auth"
)

// TestAuthzMatrix is the completeness gate for Task 3: it drives every one
// of the 17 management routes wired up in NewRouter (every route under the
// requireUser group except GET /auth/me, which has no project/org concept
// to guard) through a real router, as three different callers:
//
//   - an outsider: a member of org B, never added to org A, which owns
//     every project/environment/key this test touches. Must get 404 —
//     existence hidden from a non-member, same as a genuinely missing row.
//   - a member of org A whose role falls short of the route's minimum.
//     Must get 403. (Skipped for the handful of routes whose minimum is
//     viewer, the lowest role there is — there's no role "below" it.)
//   - a member of org A at or above the route's minimum. Must succeed.
//
// If any handler is missing its authorize* call, the outsider and/or
// below-role case for that route will (correctly) fail here — that's the
// whole point: this test is the thing that catches a handler someone forgot
// to wire up.
func TestAuthzMatrix(t *testing.T) {
	fx := seedAuthzFixture(t)
	authr := auth.New("test-secret", false)
	router := NewRouter(Deps{Repo: fx.repo, Auth: authr})

	issue := func(userID uuid.UUID) string {
		t.Helper()
		tok, err := authr.Issue(userID)
		if err != nil {
			t.Fatalf("issue session: %v", err)
		}
		return tok
	}
	viewerCookie := issue(fx.viewer.ID)
	editorCookie := issue(fx.editor.ID)
	adminCookie := issue(fx.admin.ID)
	ownerCookie := issue(fx.owner.ID)
	outsiderCookie := issue(fx.outsider.ID)

	projectPath := "/v1/projects/" + fx.project.ID.String()
	envPath := "/v1/environments/" + fx.env.ID.String()

	// assertRoute drives one route as the outsider (want 404), then — unless
	// belowCookie is empty, for the viewer-minimum routes with no role
	// below viewer to test — as a member whose role falls short (want 403),
	// then as a member at/above the minimum (want wantSuccess).
	assertRoute := func(t *testing.T, method, path, body, belowCookie, successCookie string, wantSuccess int) {
		t.Helper()
		if rec := doAuthzRequest(router, method, path, body, outsiderCookie); rec.Code != http.StatusNotFound {
			t.Errorf("%s %s as outsider = %d, want 404, body=%s", method, path, rec.Code, rec.Body.String())
		}
		if belowCookie != "" {
			if rec := doAuthzRequest(router, method, path, body, belowCookie); rec.Code != http.StatusForbidden {
				t.Errorf("%s %s as below-role member = %d, want 403, body=%s", method, path, rec.Code, rec.Body.String())
			}
		}
		if rec := doAuthzRequest(router, method, path, body, successCookie); rec.Code != wantSuccess {
			t.Errorf("%s %s as sufficiently-privileged member = %d, want %d, body=%s", method, path, rec.Code, wantSuccess, rec.Body.String())
		}
	}

	// --- viewer-minimum routes: no role sits below viewer, so there is no
	// below-role case for any of these — only outsider(404) and
	// viewer-or-above(success). ---

	t.Run("GET /projects/{projectID}", func(t *testing.T) {
		assertRoute(t, http.MethodGet, projectPath, "", "", viewerCookie, http.StatusOK)
	})
	t.Run("GET /projects/{projectID}/environments", func(t *testing.T) {
		assertRoute(t, http.MethodGet, projectPath+"/environments", "", "", viewerCookie, http.StatusOK)
	})
	t.Run("GET /environments/{envID}", func(t *testing.T) {
		assertRoute(t, http.MethodGet, envPath, "", "", viewerCookie, http.StatusOK)
	})
	t.Run("GET /environments/{envID}/api-keys", func(t *testing.T) {
		assertRoute(t, http.MethodGet, envPath+"/api-keys", "", "", viewerCookie, http.StatusOK)
	})
	t.Run("GET /projects/{projectID}/schema", func(t *testing.T) {
		assertRoute(t, http.MethodGet, projectPath+"/schema", "", "", viewerCookie, http.StatusOK)
	})
	t.Run("GET /projects/{projectID}/audit", func(t *testing.T) {
		assertRoute(t, http.MethodGet, projectPath+"/audit", "", "", viewerCookie, http.StatusOK)
	})
	t.Run("GET /environments/{envID}/versions", func(t *testing.T) {
		assertRoute(t, http.MethodGet, envPath+"/versions", "", "", viewerCookie, http.StatusOK)
	})

	// --- admin-minimum routes (via authorizeProject): below role is editor. ---

	t.Run("PATCH /projects/{projectID}", func(t *testing.T) {
		assertRoute(t, http.MethodPatch, projectPath, `{"name":"Acme Renamed"}`, editorCookie, adminCookie, http.StatusOK)
	})
	t.Run("POST /projects/{projectID}/environments", func(t *testing.T) {
		assertRoute(t, http.MethodPost, projectPath+"/environments", `{"name":"matrix-env"}`, editorCookie, adminCookie, http.StatusCreated)
	})
	t.Run("PUT /projects/{projectID}/schema", func(t *testing.T) {
		assertRoute(t, http.MethodPut, projectPath+"/schema", `{"fields":[]}`, editorCookie, adminCookie, http.StatusOK)
	})
	t.Run("POST /environments/{envID}/api-keys", func(t *testing.T) {
		assertRoute(t, http.MethodPost, envPath+"/api-keys", `{"name":"matrix-key"}`, editorCookie, adminCookie, http.StatusCreated)
	})

	// --- editor-minimum routes (via authorizeEnv): below role is viewer.
	// The values PUT below seeds version 1, which both the values GET and
	// the rollback case depend on. ---

	t.Run("PUT /environments/{envID}/values", func(t *testing.T) {
		assertRoute(t, http.MethodPut, envPath+"/values", `{"values":{}}`, viewerCookie, editorCookie, http.StatusOK)
	})
	t.Run("GET /environments/{envID}/values", func(t *testing.T) {
		assertRoute(t, http.MethodGet, envPath+"/values", "", "", viewerCookie, http.StatusOK)
	})
	t.Run("POST /environments/{envID}/rollback", func(t *testing.T) {
		assertRoute(t, http.MethodPost, envPath+"/rollback", `{"version":1}`, viewerCookie, editorCookie, http.StatusOK)
	})

	// --- DELETE api-key: admin minimum, but exercised with a fresh key
	// each time so the success case has a real row to revoke. The
	// outsider/below-role attempts never reach the key lookup (the
	// environment-level authz check runs first), so a key that doesn't
	// exist at all works fine for them. ---

	t.Run("DELETE /environments/{envID}/api-keys/{keyID}", func(t *testing.T) {
		missingKeyPath := envPath + "/api-keys/" + uuid.New().String()
		if rec := doAuthzRequest(router, http.MethodDelete, missingKeyPath, "", outsiderCookie); rec.Code != http.StatusNotFound {
			t.Errorf("DELETE api-key as outsider = %d, want 404, body=%s", rec.Code, rec.Body.String())
		}
		if rec := doAuthzRequest(router, http.MethodDelete, missingKeyPath, "", editorCookie); rec.Code != http.StatusForbidden {
			t.Errorf("DELETE api-key as editor (below admin) = %d, want 403, body=%s", rec.Code, rec.Body.String())
		}
		createRec := doAuthzRequest(router, http.MethodPost, envPath+"/api-keys", `{"name":"matrix-revoke"}`, ownerCookie)
		if createRec.Code != http.StatusCreated {
			t.Fatalf("create key to revoke = %d, want 201, body=%s", createRec.Code, createRec.Body.String())
		}
		var created map[string]any
		if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
			t.Fatalf("decode create key: %v", err)
		}
		keyID, _ := created["id"].(string)
		if keyID == "" {
			t.Fatal("create key returned no id")
		}
		if rec := doAuthzRequest(router, http.MethodDelete, envPath+"/api-keys/"+keyID, "", ownerCookie); rec.Code != http.StatusNoContent {
			t.Errorf("DELETE api-key as owner (>= admin) = %d, want 204, body=%s", rec.Code, rec.Body.String())
		}
	})

	// --- POST /projects: org-level (authorizeOrg), not project-scoped.
	// Below role is editor, success is admin. ---

	t.Run("POST /projects", func(t *testing.T) {
		body := `{"name":"Matrix Project","slug":"matrix-project","organizationId":"` + fx.orgA.ID.String() + `"}`
		assertRoute(t, http.MethodPost, "/v1/projects", body, editorCookie, adminCookie, http.StatusCreated)
	})

	// --- GET /projects: the one route with no authorize call at all — it
	// scopes at the query level instead. An outsider (member of org B only)
	// must see no org-A projects (org B has none of its own here, so their
	// list is empty); a member of org A must see fx.project. ---

	t.Run("GET /projects (scoped, not authorized)", func(t *testing.T) {
		outsiderRec := doAuthzRequest(router, http.MethodGet, "/v1/projects", "", outsiderCookie)
		if outsiderRec.Code != http.StatusOK {
			t.Fatalf("GET /projects as outsider = %d, want 200, body=%s", outsiderRec.Code, outsiderRec.Body.String())
		}
		var outsiderList []map[string]any
		if err := json.NewDecoder(outsiderRec.Body).Decode(&outsiderList); err != nil {
			t.Fatalf("decode outsider project list: %v", err)
		}
		if len(outsiderList) != 0 {
			t.Errorf("GET /projects as outsider (member of org B only) = %d projects, want 0: %v", len(outsiderList), outsiderList)
		}

		memberRec := doAuthzRequest(router, http.MethodGet, "/v1/projects", "", viewerCookie)
		if memberRec.Code != http.StatusOK {
			t.Fatalf("GET /projects as org-A member = %d, want 200, body=%s", memberRec.Code, memberRec.Body.String())
		}
		var memberList []map[string]any
		if err := json.NewDecoder(memberRec.Body).Decode(&memberList); err != nil {
			t.Fatalf("decode member project list: %v", err)
		}
		found := false
		for _, p := range memberList {
			if id, _ := p["id"].(string); id == fx.project.ID.String() {
				found = true
			}
		}
		if !found {
			t.Errorf("GET /projects as org-A member missing fx.project (%s): %v", fx.project.ID, memberList)
		}
	})
}

// doAuthzRequest is a small httptest request helper, local to this file:
// authz_matrix_test.go lives in package api (like authorize_test.go, to
// reach the unexported authorize* helpers indirectly via NewRouter and
// Deps), so it can't reuse the post/get/put/patch helpers defined in
// package api_test's auth_handlers_test.go.
func doAuthzRequest(h http.Handler, method, path, body, cookie string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: auth.CookieName(), Value: cookie})
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}
