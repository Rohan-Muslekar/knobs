package api_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Rohan-Muslekar/knobs/internal/api"
)

func TestServesIndexAtRoot(t *testing.T) {
	router := api.NewRouter(api.Deps{})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body, _ := io.ReadAll(rec.Body)
	if !strings.Contains(string(body), "<div id=\"root\">") {
		t.Fatalf("index body missing root div: %q", string(body))
	}
}

func TestUnknownRouteFallsBackToIndex(t *testing.T) {
	router := api.NewRouter(api.Deps{})

	req := httptest.NewRequest(http.MethodGet, "/projects/some-client-route", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("SPA fallback status = %d, want 200", rec.Code)
	}
	body, _ := io.ReadAll(rec.Body)
	if !strings.Contains(string(body), "<div id=\"root\">") {
		t.Fatalf("SPA fallback body missing root div: %q", string(body))
	}
}

func TestUnknownV1RouteReturnsJSON404(t *testing.T) {
	router := api.NewRouter(api.Deps{})

	req := httptest.NewRequest(http.MethodGet, "/v1/does-not-exist", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("content-type = %q, want json (API 404 must not fall through to SPA)", ct)
	}
	body, _ := io.ReadAll(rec.Body)
	if strings.Contains(string(body), "<div id=\"root\">") {
		t.Fatalf("api 404 returned the SPA shell: %q", string(body))
	}
}

func TestHealthzStillJSON(t *testing.T) {
	router := api.NewRouter(api.Deps{})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("healthz content-type = %q, want json (API routes must win over SPA)", ct)
	}
}
