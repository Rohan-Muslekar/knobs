// Package api builds the Knobs HTTP surface.
package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/Rohan-Muslekar/knobs/web"
)

// Deps carries everything the HTTP layer needs. It grows as later phases add
// a store, auth, and the SSE hub.
type Deps struct{}

// NewRouter wires all routes and returns the root handler.
func NewRouter(deps Deps) chi.Router {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)

	r.Get("/healthz", healthHandler)

	// API namespace: unmatched /v1/* returns JSON 404, never the SPA HTML shell.
	r.Route("/v1", func(r chi.Router) {
		r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
		})
	})

	// SPA catch-all mounted last; explicit API routes above take precedence.
	assets := web.Assets()
	r.NotFound(spaHandler(assets))
	r.Get("/", spaHandler(assets))

	return r
}
