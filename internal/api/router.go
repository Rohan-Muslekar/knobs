// Package api builds the Knobs HTTP surface.
package api

import (
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
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

	return r
}
