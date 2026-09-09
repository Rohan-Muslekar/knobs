//go:build integration

package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Rohan-Muslekar/knobs/internal/api"
	"github.com/Rohan-Muslekar/knobs/internal/auth"
	"github.com/Rohan-Muslekar/knobs/internal/store"
)

func TestLoginMeLogoutFlow(t *testing.T) {
	repo := store.New(migratedPool(t)) // shared helper, see note below
	authr := auth.New("test-secret", false)
	hash, _ := authr.Hash("pw")
	if _, err := repo.CreateUser(t.Context(), repo.Pool(), "a@x.com", hash); err != nil {
		t.Fatalf("seed: %v", err)
	}
	router := api.NewRouter(api.Deps{Repo: repo, Auth: authr})

	// Wrong password -> 401.
	if rec := post(router, "/v1/auth/login", `{"email":"a@x.com","password":"nope"}`, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad login status = %d, want 401", rec.Code)
	}

	// Unknown user -> 401 with the same body as a wrong password, proving
	// the two branches are indistinguishable to a caller.
	rec := post(router, "/v1/auth/login", `{"email":"nobody@x.com","password":"whatever"}`, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unknown user login status = %d, want 401", rec.Code)
	}
	var errBody map[string]string
	_ = json.NewDecoder(rec.Body).Decode(&errBody)
	if errBody["error"] != "invalid credentials" {
		t.Fatalf("unknown user login body = %v, want {error: invalid credentials}", errBody)
	}

	// Correct login -> 200 + cookie.
	rec = post(router, "/v1/auth/login", `{"email":"a@x.com","password":"pw"}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200", rec.Code)
	}
	cookie := rec.Result().Cookies()[0]
	if cookie.Name != auth.CookieName() || cookie.Value == "" {
		t.Fatalf("session cookie not set: %+v", cookie)
	}

	// /me without cookie -> 401.
	if rec := get(router, "/v1/auth/me", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("me without cookie = %d, want 401", rec.Code)
	}
	// /me with cookie -> 200 and the right email.
	rec = get(router, "/v1/auth/me", cookie.Value)
	if rec.Code != http.StatusOK {
		t.Fatalf("me status = %d, want 200", rec.Code)
	}
	var body map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&body)
	if body["email"] != "a@x.com" {
		t.Fatalf("me email = %v, want a@x.com", body["email"])
	}

	// Logout clears the cookie.
	rec = post(router, "/v1/auth/logout", "", cookie.Value)
	if rec.Code != http.StatusOK {
		t.Fatalf("logout status = %d, want 200", rec.Code)
	}
	logoutCookies := rec.Result().Cookies()
	if len(logoutCookies) == 0 || logoutCookies[0].Name != auth.CookieName() {
		t.Fatalf("logout did not set a %s cookie: %+v", auth.CookieName(), logoutCookies)
	}
	if logoutCookies[0].MaxAge >= 0 {
		t.Fatalf("logout cookie MaxAge = %d, want < 0 (expired)", logoutCookies[0].MaxAge)
	}
}

// TestLoginRateLimiting fires rapid bad logins from the same client and
// asserts it gets cut off with 429 once it exceeds the default budget (10
// attempts / 15 min, see router.go's loginMaxAttempts), that the offending
// client stays blocked even once it types the right password, and that a
// good login from a DIFFERENT client (a different RemoteAddr) is
// unaffected.
func TestLoginRateLimiting(t *testing.T) {
	repo := store.New(migratedPool(t))
	authr := auth.New("test-secret", false)
	hash, _ := authr.Hash("pw")
	if _, err := repo.CreateUser(t.Context(), repo.Pool(), "a@x.com", hash); err != nil {
		t.Fatalf("seed: %v", err)
	}

	router := api.NewRouter(api.Deps{Repo: repo, Auth: authr})

	const maxAttempts = 10
	attacker := "10.0.0.1:1111"
	for i := 1; i <= maxAttempts; i++ {
		rec := postFrom(router, "/v1/auth/login", `{"email":"a@x.com","password":"nope"}`, "", attacker)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("bad login #%d status = %d, want 401", i, rec.Code)
		}
	}

	// The (maxAttempts+1)th bad attempt from the same client is over
	// budget -> 429.
	rec := postFrom(router, "/v1/auth/login", `{"email":"a@x.com","password":"nope"}`, "", attacker)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("login #%d status = %d, want 429", maxAttempts+1, rec.Code)
	}
	var errBody map[string]string
	_ = json.NewDecoder(rec.Body).Decode(&errBody)
	if errBody["error"] != "too many login attempts, try again later" {
		t.Fatalf("429 body = %v, want the rate-limit message", errBody)
	}

	// Even the CORRECT password is now blocked for this client — it's over
	// budget regardless of credential validity.
	rec = postFrom(router, "/v1/auth/login", `{"email":"a@x.com","password":"pw"}`, "", attacker)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("correct-password login while over budget = %d, want 429", rec.Code)
	}

	// A DIFFERENT client (different RemoteAddr) is unaffected and can still
	// log in successfully.
	other := "10.0.0.2:2222"
	rec = postFrom(router, "/v1/auth/login", `{"email":"a@x.com","password":"pw"}`, "", other)
	if rec.Code != http.StatusOK {
		t.Fatalf("different client's login = %d, want 200", rec.Code)
	}
}

// small request helpers used across api integration tests
func post(h http.Handler, path, body, cookie string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: auth.CookieName(), Value: cookie})
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// postFrom is post plus an explicit RemoteAddr, for tests that need to
// simulate requests from distinct clients (e.g. rate-limit tests).
func postFrom(h http.Handler, path, body, cookie, remoteAddr string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = remoteAddr
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: auth.CookieName(), Value: cookie})
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func get(h http.Handler, path, cookie string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: auth.CookieName(), Value: cookie})
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func patch(h http.Handler, path, body, cookie string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPatch, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: auth.CookieName(), Value: cookie})
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func put(h http.Handler, path, body, cookie string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPut, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: auth.CookieName(), Value: cookie})
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}
