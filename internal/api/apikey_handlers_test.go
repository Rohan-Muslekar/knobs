//go:build integration

package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Rohan-Muslekar/knobs/internal/apikey"
	"github.com/Rohan-Muslekar/knobs/internal/auth"
)

// hexHashRe matches a bare 64-char hex string, the shape of a sha256 hex
// digest. Used to prove the audit diff for apikey.create carries no hash.
var hexHashRe = regexp.MustCompile(`\b[0-9a-f]{64}\b`)

// del is a small request helper, matching the shape of post/get/put/patch in
// auth_handlers_test.go, added here because this is the first test that
// needs DELETE.
func del(h http.Handler, path, cookie string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodDelete, path, nil)
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: auth.CookieName(), Value: cookie})
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestApiKeysAPI(t *testing.T) {
	router, cookie, repo, orgID := seededRouter(t)

	projRec := post(router, "/v1/projects", createProjectBody(orgID, "Acme", "acme"), cookie)
	if projRec.Code != http.StatusCreated {
		t.Fatalf("create project = %d, want 201, body=%s", projRec.Code, projRec.Body.String())
	}
	var proj map[string]any
	_ = json.NewDecoder(projRec.Body).Decode(&proj)
	projectID, _ := proj["id"].(string)

	envRec := post(router, "/v1/projects/"+projectID+"/environments", `{"name":"staging"}`, cookie)
	if envRec.Code != http.StatusCreated {
		t.Fatalf("create env = %d, want 201, body=%s", envRec.Code, envRec.Body.String())
	}
	var env map[string]any
	_ = json.NewDecoder(envRec.Body).Decode(&env)
	envID, _ := env["id"].(string)
	if envID == "" {
		t.Fatal("no env id returned")
	}

	// Unauthenticated create is rejected.
	if rec := post(router, "/v1/environments/"+envID+"/api-keys", `{"name":"ci"}`, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth create = %d, want 401", rec.Code)
	}

	// Missing name is a 422.
	if rec := post(router, "/v1/environments/"+envID+"/api-keys", `{"name":""}`, cookie); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("empty name create = %d, want 422, body=%s", rec.Code, rec.Body.String())
	}

	// Create under a missing environment is a 404.
	if rec := post(router, "/v1/environments/"+uuid.New().String()+"/api-keys", `{"name":"ci"}`, cookie); rec.Code != http.StatusNotFound {
		t.Fatalf("create under missing env = %d, want 404", rec.Code)
	}

	createRec := post(router, "/v1/environments/"+envID+"/api-keys", `{"name":"ci"}`, cookie)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create key = %d, want 201, body=%s", createRec.Code, createRec.Body.String())
	}
	createBody := createRec.Body.String()
	var created map[string]any
	if err := json.Unmarshal([]byte(createBody), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	keyID, _ := created["id"].(string)
	if keyID == "" {
		t.Fatal("no key id returned")
	}
	if name, _ := created["name"].(string); name != "ci" {
		t.Fatalf("created name = %q, want %q", name, "ci")
	}
	if _, ok := created["createdAt"]; !ok {
		t.Fatalf("created response missing createdAt: %v", created)
	}
	plaintext, _ := created["key"].(string)
	if !strings.HasPrefix(plaintext, "knobs_") {
		t.Fatalf("created key = %q, want knobs_-prefixed", plaintext)
	}
	// The plaintext must appear exactly once in the response body (i.e. only
	// under the "key" field, never duplicated elsewhere e.g. in a hash).
	if n := strings.Count(createBody, plaintext); n != 1 {
		t.Fatalf("plaintext key appears %d times in create response, want 1: %s", n, createBody)
	}

	// The plaintext hashes to the row that was actually persisted.
	hash := apikey.Hash(plaintext)
	stored, err := repo.ApiKeyByHash(t.Context(), repo.Pool(), hash)
	if err != nil {
		t.Fatalf("ApiKeyByHash: %v", err)
	}
	if stored.ID.String() != keyID {
		t.Fatalf("ApiKeyByHash id = %s, want %s", stored.ID, keyID)
	}

	// List: no key or hash field present, ever.
	listRec := get(router, "/v1/environments/"+envID+"/api-keys", cookie)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list = %d, want 200, body=%s", listRec.Code, listRec.Body.String())
	}
	listBody := listRec.Body.String()
	if strings.Contains(listBody, plaintext) {
		t.Fatalf("list response leaks plaintext key: %s", listBody)
	}
	var list []map[string]any
	if err := json.Unmarshal([]byte(listBody), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("list len = %d, want 1", len(list))
	}
	for _, k := range list {
		if _, ok := k["key"]; ok {
			t.Fatalf("list entry has a key field: %v", k)
		}
		if _, ok := k["hash"]; ok {
			t.Fatalf("list entry has a hash field: %v", k)
		}
		for _, want := range []string{"id", "name", "scope", "createdAt"} {
			if _, ok := k[want]; !ok {
				t.Fatalf("list entry missing %q: %v", want, k)
			}
		}
	}

	// List under a missing environment is a 404.
	if rec := get(router, "/v1/environments/"+uuid.New().String()+"/api-keys", cookie); rec.Code != http.StatusNotFound {
		t.Fatalf("list under missing env = %d, want 404", rec.Code)
	}

	// Revoke a nonexistent key is a 404.
	if rec := del(router, "/v1/environments/"+envID+"/api-keys/"+uuid.New().String(), cookie); rec.Code != http.StatusNotFound {
		t.Fatalf("revoke missing key = %d, want 404", rec.Code)
	}

	// Revoke the real key -> 204, then list is empty.
	if rec := del(router, "/v1/environments/"+envID+"/api-keys/"+keyID, cookie); rec.Code != http.StatusNoContent {
		t.Fatalf("revoke = %d, want 204, body=%s", rec.Code, rec.Body.String())
	}
	afterListRec := get(router, "/v1/environments/"+envID+"/api-keys", cookie)
	if afterListRec.Code != http.StatusOK {
		t.Fatalf("list after revoke = %d, want 200", afterListRec.Code)
	}
	var afterList []map[string]any
	_ = json.NewDecoder(afterListRec.Body).Decode(&afterList)
	if len(afterList) != 0 {
		t.Fatalf("list after revoke len = %d, want 0", len(afterList))
	}

	// Audit read-back: apikey.create and apikey.revoke both landed, and the
	// create entry's diff leaks neither the plaintext key nor its hash.
	rows, err := repo.Pool().Query(t.Context(),
		`SELECT action, diff FROM audit_log WHERE project_id = $1 ORDER BY at`, projectID)
	if err != nil {
		t.Fatalf("audit query: %v", err)
	}
	type entry struct {
		action string
		diff   string
	}
	var entries []entry
	for rows.Next() {
		var e entry
		var diff []byte
		if err := rows.Scan(&e.action, &diff); err != nil {
			t.Fatalf("audit scan: %v", err)
		}
		e.diff = string(diff)
		entries = append(entries, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("audit rows: %v", err)
	}

	wantActions := map[string]bool{"apikey.create": false, "apikey.revoke": false}
	for _, e := range entries {
		if _, ok := wantActions[e.action]; ok {
			wantActions[e.action] = true
		}
		if e.action == "apikey.create" {
			if strings.Contains(e.diff, "knobs_") {
				t.Fatalf("apikey.create audit diff leaks plaintext prefix: %s", e.diff)
			}
			if strings.Contains(e.diff, plaintext) {
				t.Fatalf("apikey.create audit diff leaks plaintext key: %s", e.diff)
			}
			if strings.Contains(e.diff, hash) {
				t.Fatalf("apikey.create audit diff leaks hash: %s", e.diff)
			}
			if hexHashRe.MatchString(e.diff) {
				t.Fatalf("apikey.create audit diff contains a 64-hex-char hash-shaped string: %s", e.diff)
			}
		}
	}
	for action, seen := range wantActions {
		if !seen {
			t.Fatalf("audit_log missing action %q, entries=%v", action, entries)
		}
	}
}
