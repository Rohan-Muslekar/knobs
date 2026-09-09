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
			// The /environments/{envID}... routes below are addressable by
			// bare env id with no project-scope/ownership check — fine under
			// single-tenant P1, but they'll need project-scoped authorization
			// once P1b introduces per-user/per-project scoping.
			r.Get("/environments/{envID}", deps.handleGetEnvironment)
			r.Post("/environments/{envID}/api-keys", deps.handleCreateApiKey)
			r.Get("/environments/{envID}/api-keys", deps.handleListApiKeys)
			r.Delete("/environments/{envID}/api-keys/{keyID}", deps.handleRevokeApiKey)
			r.Get("/projects/{projectID}/schema", deps.handleGetSchema)
			r.Put("/projects/{projectID}/schema", deps.handlePutSchema)
			r.Get("/environments/{envID}/values", deps.handleGetValues)
			r.Put("/environments/{envID}/values", deps.handlePutValues)
			r.Get("/environments/{envID}/versions", deps.handleListVersions)
			r.Post("/environments/{envID}/rollback", deps.handleRollback)
			r.Get("/projects/{projectID}/audit", deps.handleListAudit)
		})

		// Delivery routes: machine consumers (SDKs) authenticate with a
		// Bearer API key, never a session cookie. This is a separate
		// r.Group from the requireUser one above, each with its own
		// middleware stack, so neither credential can satisfy the other's
		// guard — a session cookie can't reach /v1/snapshot, and an API key
		// can't reach the management routes.
		r.Group(func(r chi.Router) {
			r.Use(apiKeyGuard(deps))
			r.Get("/snapshot", deps.handleSnapshot)
		})
	})

	// SPA catch-all mounted last; explicit API routes above take precedence.
	assets := web.Assets()
	r.NotFound(spaHandler(assets))
	r.Get("/", spaHandler(assets))

	return r
}
