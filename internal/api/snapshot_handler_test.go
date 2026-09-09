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

// bearer issues a GET with an `Authorization: Bearer <token>` header, or no
// Authorization header at all when token is "". Distinct from get() in
// auth_handlers_test.go, which authenticates via session cookie — this is
// the delivery auth path, and the two must never satisfy each other.
func bearer(h http.Handler, path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestSnapshotAPI(t *testing.T) {
	router, cookie, repo := seededRouter(t)

	projRec := post(router, "/v1/projects", `{"name":"Acme","slug":"acme"}`, cookie)
	if projRec.Code != http.StatusCreated {
		t.Fatalf("create project = %d, want 201, body=%s", projRec.Code, projRec.Body.String())
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

	// Two environments in the same project, each with its own values, so the
	// scope assertions below actually distinguish "this key's env" from "any
	// env in the project".
	envARec := post(router, "/v1/projects/"+projectID+"/environments", `{"name":"staging"}`, cookie)
	if envARec.Code != http.StatusCreated {
		t.Fatalf("create env A = %d, want 201, body=%s", envARec.Code, envARec.Body.String())
	}
	var envA map[string]any
	_ = json.NewDecoder(envARec.Body).Decode(&envA)
	envAID, _ := envA["id"].(string)

	envBRec := post(router, "/v1/projects/"+projectID+"/environments", `{"name":"prod"}`, cookie)
	if envBRec.Code != http.StatusCreated {
		t.Fatalf("create env B = %d, want 201, body=%s", envBRec.Code, envBRec.Body.String())
	}
	var envB map[string]any
	_ = json.NewDecoder(envBRec.Body).Decode(&envB)
	envBID, _ := envB["id"].(string)

	// Env with no values at all, to exercise the 404 "no values set" path.
	envCRec := post(router, "/v1/projects/"+projectID+"/environments", `{"name":"empty"}`, cookie)
	if envCRec.Code != http.StatusCreated {
		t.Fatalf("create env C = %d, want 201, body=%s", envCRec.Code, envCRec.Body.String())
	}
	var envC map[string]any
	_ = json.NewDecoder(envCRec.Body).Decode(&envC)
	envCID, _ := envC["id"].(string)

	if rec := put(router, "/v1/environments/"+envAID+"/values", `{"values":{"maxRetries":3}}`, cookie); rec.Code != http.StatusOK {
		t.Fatalf("put values A = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if rec := put(router, "/v1/environments/"+envBID+"/values", `{"values":{"maxRetries":4}}`, cookie); rec.Code != http.StatusOK {
		t.Fatalf("put values B = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	createKey := func(envID string) string {
		rec := post(router, "/v1/environments/"+envID+"/api-keys", `{"name":"sdk"}`, cookie)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create key for %s = %d, want 201, body=%s", envID, rec.Code, rec.Body.String())
		}
		var body map[string]any
		_ = json.NewDecoder(rec.Body).Decode(&body)
		key, _ := body["key"].(string)
		if key == "" {
			t.Fatalf("no plaintext key returned for %s", envID)
		}
		return key
	}
	keyA := createKey(envAID)
	keyB := createKey(envBID)
	keyC := createKey(envCID)

	// The hash a fresh project's schema definition should produce, computed
	// independently of the handler via the same delivery.SchemaHash helper,
	// so the test proves the response carries the real hash, not a stub.
	cs, err := repo.GetOrInitSchema(t.Context(), repo.Pool(), uuid.MustParse(projectID))
	if err != nil {
		t.Fatalf("GetOrInitSchema: %v", err)
	}
	wantHash := delivery.SchemaHash(cs.Definition)

	// Happy path: env A's key returns env A's snapshot.
	snapARec := bearer(router, "/v1/snapshot", keyA)
	if snapARec.Code != http.StatusOK {
		t.Fatalf("snapshot A = %d, want 200, body=%s", snapARec.Code, snapARec.Body.String())
	}
	var snapA map[string]any
	_ = json.NewDecoder(snapARec.Body).Decode(&snapA)
	if v, _ := snapA["version"].(float64); v != 1 {
		t.Fatalf("snapshot A version = %v, want 1", snapA["version"])
	}
	if h, _ := snapA["schemaHash"].(string); h != wantHash {
		t.Fatalf("snapshot A schemaHash = %q, want %q", h, wantHash)
	}
	// A single PUT values is env A's first-ever write, so its delivery
	// revision is 1 — same as its version here, since nothing has diverged
	// them yet (that divergence is what TestSnapshotRevisionMonotonicAcrossRollback
	// covers).
	if rev, _ := snapA["revision"].(float64); rev != 1 {
		t.Fatalf("snapshot A revision = %v, want 1", snapA["revision"])
	}
	valuesA, _ := snapA["values"].(map[string]any)
	if mr, _ := valuesA["maxRetries"].(float64); mr != 3 {
		t.Fatalf("snapshot A values.maxRetries = %v, want 3", valuesA["maxRetries"])
	}

	// Env B's key returns env B's own snapshot, not A's — proves the key
	// scopes the response rather than returning some shared/global state.
	snapBRec := bearer(router, "/v1/snapshot", keyB)
	if snapBRec.Code != http.StatusOK {
		t.Fatalf("snapshot B = %d, want 200, body=%s", snapBRec.Code, snapBRec.Body.String())
	}
	var snapB map[string]any
	_ = json.NewDecoder(snapBRec.Body).Decode(&snapB)
	valuesB, _ := snapB["values"].(map[string]any)
	if mr, _ := valuesB["maxRetries"].(float64); mr != 4 {
		t.Fatalf("snapshot B values.maxRetries = %v, want 4", valuesB["maxRetries"])
	}

	// A `?env=` query param that names a different environment than the
	// key's own must be ignored: the key alone decides which env comes
	// back.
	mismatchRec := bearer(router, "/v1/snapshot?env="+envBID, keyA)
	if mismatchRec.Code != http.StatusOK {
		t.Fatalf("snapshot A with mismatched ?env= = %d, want 200, body=%s", mismatchRec.Code, mismatchRec.Body.String())
	}
	var mismatch map[string]any
	_ = json.NewDecoder(mismatchRec.Body).Decode(&mismatch)
	mismatchValues, _ := mismatch["values"].(map[string]any)
	if mr, _ := mismatchValues["maxRetries"].(float64); mr != 3 {
		t.Fatalf("snapshot with mismatched ?env= returned = %v, want A's value 3 (env param must be ignored)", mismatchValues["maxRetries"])
	}

	// No Authorization header at all -> 401.
	if rec := bearer(router, "/v1/snapshot", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no auth header = %d, want 401, body=%s", rec.Code, rec.Body.String())
	}

	// A well-formed but unknown key -> 401.
	if rec := bearer(router, "/v1/snapshot", "knobs_not-a-real-key"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unknown key = %d, want 401, body=%s", rec.Code, rec.Body.String())
	}

	// A malformed Authorization header (not "Bearer <token>") -> 401.
	badHeaderReq := httptest.NewRequest(http.MethodGet, "/v1/snapshot", nil)
	badHeaderReq.Header.Set("Authorization", "Basic "+keyA)
	badHeaderRec := httptest.NewRecorder()
	router.ServeHTTP(badHeaderRec, badHeaderReq)
	if badHeaderRec.Code != http.StatusUnauthorized {
		t.Fatalf("malformed auth header = %d, want 401, body=%s", badHeaderRec.Code, badHeaderRec.Body.String())
	}

	// A valid session cookie, with no Bearer key, is not delivery auth -> 401.
	cookieOnlyReq := httptest.NewRequest(http.MethodGet, "/v1/snapshot", nil)
	cookieOnlyReq.AddCookie(&http.Cookie{Name: auth.CookieName(), Value: cookie})
	cookieOnlyRec := httptest.NewRecorder()
	router.ServeHTTP(cookieOnlyRec, cookieOnlyReq)
	if cookieOnlyRec.Code != http.StatusUnauthorized {
		t.Fatalf("session cookie only = %d, want 401, body=%s", cookieOnlyRec.Code, cookieOnlyRec.Body.String())
	}

	// Conversely, a Bearer key is not a session: it must not reach a
	// requireUser-guarded management route.
	mgmtReq := httptest.NewRequest(http.MethodGet, "/v1/environments/"+envAID, nil)
	mgmtReq.Header.Set("Authorization", "Bearer "+keyA)
	mgmtRec := httptest.NewRecorder()
	router.ServeHTTP(mgmtRec, mgmtReq)
	if mgmtRec.Code != http.StatusUnauthorized {
		t.Fatalf("bearer key against management route = %d, want 401, body=%s", mgmtRec.Code, mgmtRec.Body.String())
	}

	// Env with no current version -> 404 "no values set".
	emptyRec := bearer(router, "/v1/snapshot", keyC)
	if emptyRec.Code != http.StatusNotFound {
		t.Fatalf("snapshot for env with no values = %d, want 404, body=%s", emptyRec.Code, emptyRec.Body.String())
	}
	var emptyBody map[string]string
	_ = json.NewDecoder(emptyRec.Body).Decode(&emptyBody)
	if emptyBody["error"] != "no values set" {
		t.Fatalf("snapshot for env with no values body = %v, want {error: no values set}", emptyBody)
	}
}

// TestSnapshotRevisionMonotonicAcrossRollback is the core regression test
// for the monotonic delivery revision: a rollback repoints
// current_version_id at an OLDER config_version, so the user-facing
// `version` in the delivered snapshot goes backwards, but `revision` — the
// axis delivery dedup/`since` gating actually uses — must keep climbing
// regardless, because SetCurrentVersion bumps delivery_revision
// unconditionally on every call it makes, rollback included.
func TestSnapshotRevisionMonotonicAcrossRollback(t *testing.T) {
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

	keyRec := post(router, "/v1/environments/"+envID+"/api-keys", `{"name":"sdk"}`, cookie)
	if keyRec.Code != http.StatusCreated {
		t.Fatalf("create key = %d, want 201, body=%s", keyRec.Code, keyRec.Body.String())
	}
	var keyBody map[string]any
	_ = json.NewDecoder(keyRec.Body).Decode(&keyBody)
	key, _ := keyBody["key"].(string)

	// Version 1.
	if rec := put(router, "/v1/environments/"+envID+"/values", `{"values":{"maxRetries":3}}`, cookie); rec.Code != http.StatusOK {
		t.Fatalf("put values v1 = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	// Version 2 — bumps the delivery revision again.
	if rec := put(router, "/v1/environments/"+envID+"/values", `{"values":{"maxRetries":4}}`, cookie); rec.Code != http.StatusOK {
		t.Fatalf("put values v2 = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	snapAtV2Rec := bearer(router, "/v1/snapshot", key)
	if snapAtV2Rec.Code != http.StatusOK {
		t.Fatalf("snapshot at v2 = %d, want 200, body=%s", snapAtV2Rec.Code, snapAtV2Rec.Body.String())
	}
	var snapAtV2 map[string]any
	_ = json.NewDecoder(snapAtV2Rec.Body).Decode(&snapAtV2)
	if v, _ := snapAtV2["version"].(float64); v != 2 {
		t.Fatalf("snapshot at v2 version = %v, want 2", snapAtV2["version"])
	}
	revisionAtV2, _ := snapAtV2["revision"].(float64)
	if revisionAtV2 <= 0 {
		t.Fatalf("snapshot at v2 revision = %v, want > 0", snapAtV2["revision"])
	}

	// Roll back to version 1.
	if rec := post(router, "/v1/environments/"+envID+"/rollback", `{"version":1}`, cookie); rec.Code != http.StatusOK {
		t.Fatalf("rollback = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	snapAfterRollbackRec := bearer(router, "/v1/snapshot", key)
	if snapAfterRollbackRec.Code != http.StatusOK {
		t.Fatalf("snapshot after rollback = %d, want 200, body=%s", snapAfterRollbackRec.Code, snapAfterRollbackRec.Body.String())
	}
	var snapAfterRollback map[string]any
	_ = json.NewDecoder(snapAfterRollbackRec.Body).Decode(&snapAfterRollback)

	// The user-facing version went backwards...
	if v, _ := snapAfterRollback["version"].(float64); v != 1 {
		t.Fatalf("snapshot after rollback version = %v, want 1", snapAfterRollback["version"])
	}
	// ...but the delivery revision, the axis SDKs actually dedupe/gate
	// `since` on, must have kept climbing despite that.
	revisionAfterRollback, _ := snapAfterRollback["revision"].(float64)
	if revisionAfterRollback <= revisionAtV2 {
		t.Fatalf("snapshot after rollback revision = %v, want > %v (revision at version 2) even though version went 2 -> 1",
			revisionAfterRollback, revisionAtV2)
	}
}

// TestSnapshotTouchesLastUsedAt proves a delivery request through
// apiKeyGuard updates the presented key's last_used_at, so operators can
// tell a live key from a dead one. apiKeyGuard calls repo.TouchApiKey
// inline, before next.ServeHTTP, so the update is already committed by the
// time this GET returns — no polling or retry needed for a deterministic
// assertion.
func TestSnapshotTouchesLastUsedAt(t *testing.T) {
	router, cookie, repo := seededRouter(t)

	projRec := post(router, "/v1/projects", `{"name":"Acme","slug":"acme"}`, cookie)
	if projRec.Code != http.StatusCreated {
		t.Fatalf("create project = %d, want 201, body=%s", projRec.Code, projRec.Body.String())
	}
	var proj map[string]any
	_ = json.NewDecoder(projRec.Body).Decode(&proj)
	projectID, _ := proj["id"].(string)

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

	if rec := put(router, "/v1/environments/"+envID+"/values", `{"values":{"maxRetries":3}}`, cookie); rec.Code != http.StatusOK {
		t.Fatalf("put values = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	keyRec := post(router, "/v1/environments/"+envID+"/api-keys", `{"name":"sdk"}`, cookie)
	if keyRec.Code != http.StatusCreated {
		t.Fatalf("create key = %d, want 201, body=%s", keyRec.Code, keyRec.Body.String())
	}
	var keyBody map[string]any
	_ = json.NewDecoder(keyRec.Body).Decode(&keyBody)
	keyID, _ := keyBody["id"].(string)
	plaintext, _ := keyBody["key"].(string)

	// Before any delivery request, a freshly created key has never been
	// used.
	before, err := repo.ListApiKeys(t.Context(), repo.Pool(), uuid.MustParse(envID))
	if err != nil {
		t.Fatalf("ListApiKeys before: %v", err)
	}
	if len(before) != 1 {
		t.Fatalf("keys before = %d, want 1", len(before))
	}
	if before[0].LastUsedAt != nil {
		t.Fatalf("LastUsedAt before any use = %v, want nil", before[0].LastUsedAt)
	}

	if rec := bearer(router, "/v1/snapshot", plaintext); rec.Code != http.StatusOK {
		t.Fatalf("snapshot = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	after, err := repo.ListApiKeys(t.Context(), repo.Pool(), uuid.MustParse(envID))
	if err != nil {
		t.Fatalf("ListApiKeys after: %v", err)
	}
	if len(after) != 1 {
		t.Fatalf("keys after = %d, want 1", len(after))
	}
	if after[0].ID.String() != keyID {
		t.Fatalf("after[0].ID = %s, want %s", after[0].ID, keyID)
	}
	if after[0].LastUsedAt == nil {
		t.Fatal("LastUsedAt after a snapshot fetch = nil, want non-nil")
	}
}
