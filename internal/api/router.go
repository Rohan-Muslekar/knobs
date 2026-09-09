// Package api builds the Knobs HTTP surface.
package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/Rohan-Muslekar/knobs/internal/auth"
	"github.com/Rohan-Muslekar/knobs/internal/store"
	"github.com/Rohan-Muslekar/knobs/web"
)

// Deps carries everything the HTTP layer needs. It grows as later phases add
// the SSE hub.
type Deps struct {
	Repo *store.Repo
	Auth *auth.Authenticator
}

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

		// Public auth routes.
		r.Post("/auth/login", deps.handleLogin)
		r.Post("/auth/logout", deps.handleLogout)

		// Everything below requires a valid session.
		r.Group(func(r chi.Router) {
			r.Use(requireUser(deps))
			r.Get("/auth/me", deps.handleMe)
			r.Post("/projects", deps.handleCreateProject)
			r.Get("/projects", deps.handleListProjects)
			r.Get("/projects/{projectID}", deps.handleGetProject)
			r.Patch("/projects/{projectID}", deps.handlePatchProject)
			r.Post("/projects/{projectID}/environments", deps.handleCreateEnvironment)
			r.Get("/projects/{projectID}/environments", deps.handleListEnvironments)
			r.Get("/environments/{envID}", deps.handleGetEnvironment)
			// Later tasks add schema/value/version/audit routes here.
		})
	})

	// SPA catch-all mounted last; explicit API routes above take precedence.
	assets := web.Assets()
	r.NotFound(spaHandler(assets))
	r.Get("/", spaHandler(assets))

	return r
}
