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
