package app_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Rohan-Muslekar/knobs/internal/app"
)

func TestAppRouterServesHealthAndUI(t *testing.T) {
	router := app.NewRouter()

	for _, tc := range []struct {
		path       string
		wantSubstr string
	}{
		{"/healthz", "ok"},
		{"/", "<div id=\"root\">"},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want 200", tc.path, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), tc.wantSubstr) {
			t.Fatalf("%s body missing %q", tc.path, tc.wantSubstr)
		}
	}
}
