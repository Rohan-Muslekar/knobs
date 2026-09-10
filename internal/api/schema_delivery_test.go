//go:build integration

package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/Rohan-Muslekar/knobs/internal/auth"
	"github.com/Rohan-Muslekar/knobs/internal/delivery"
)

// TestSchemaDelivery proves GET /v1/schema sits behind the same apiKeyGuard
// as /v1/snapshot: a valid Bearer key returns the presented key's
// environment's project schema (field list + schemaVersion) plus a
// schemaHash equal to delivery.SchemaHash of that same definition, and
// neither a missing/invalid Bearer token nor a session cookie satisfies it.
func TestSchemaDelivery(t *testing.T) {
	router, cookie, repo, orgID := seededRouter(t)

	projRec := post(router, "/v1/projects", createProjectBody(orgID, "Acme", "acme"), cookie)
	if projRec.Code != http.StatusCreated {
		t.Fatalf("create project = %d, want 201, body=%s", projRec.Code, projRec.Body.String())
	}
	var proj map[string]any
	_ = json.NewDecoder(projRec.Body).Decode(&proj)
	projectID, _ := proj["id"].(string)
	if projectID == "" {
		t.Fatal("no project id returned")
	}

	schemaBody := `{"fields":[{"name":"maxRetries","type":"int","required":true,"max":5},{"name":"featureX","type":"bool","required":false}]}`
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

	// The hash + schema this project's definition should produce, computed
	// independently of the handler via the store + delivery.SchemaHash, so
	// this test proves the response carries the real values, not stubs.
	cs, err := repo.GetOrInitSchema(t.Context(), repo.Pool(), uuid.MustParse(projectID))
	if err != nil {
		t.Fatalf("GetOrInitSchema: %v", err)
	}
	wantHash := delivery.SchemaHash(cs.Definition)

	rec := bearer(router, "/v1/schema", key)
	if rec.Code != http.StatusOK {
		t.Fatalf("schema = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Definition struct {
			Fields []map[string]any `json:"fields"`
		} `json:"definition"`
		SchemaVersion int    `json:"schemaVersion"`
		SchemaHash    string `json:"schemaHash"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Definition.Fields) != 2 {
		t.Fatalf("fields = %d, want 2, body=%+v", len(body.Definition.Fields), body)
	}
	names := map[string]bool{}
	for _, f := range body.Definition.Fields {
		names[f["name"].(string)] = true
	}
	if !names["maxRetries"] || !names["featureX"] {
		t.Fatalf("fields = %v, want maxRetries and featureX", names)
	}
	if body.SchemaVersion != cs.SchemaVersion {
		t.Fatalf("schemaVersion = %d, want %d", body.SchemaVersion, cs.SchemaVersion)
	}
	if body.SchemaHash != wantHash {
		t.Fatalf("schemaHash = %q, want %q", body.SchemaHash, wantHash)
	}

	// No Authorization header at all -> 401.
	if rec := bearer(router, "/v1/schema", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no auth header = %d, want 401, body=%s", rec.Code, rec.Body.String())
	}

	// A well-formed but unknown key -> 401.
	if rec := bearer(router, "/v1/schema", "knobs_not-a-real-key"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unknown key = %d, want 401, body=%s", rec.Code, rec.Body.String())
	}

	// A valid session cookie, with no Bearer key, is not delivery auth -> 401.
	cookieOnlyReq := httptest.NewRequest(http.MethodGet, "/v1/schema", nil)
	cookieOnlyReq.AddCookie(&http.Cookie{Name: auth.CookieName(), Value: cookie})
	cookieOnlyRec := httptest.NewRecorder()
	router.ServeHTTP(cookieOnlyRec, cookieOnlyReq)
	if cookieOnlyRec.Code != http.StatusUnauthorized {
		t.Fatalf("session cookie only = %d, want 401, body=%s", cookieOnlyRec.Code, cookieOnlyRec.Body.String())
	}
}
