// Package api builds the Knobs HTTP surface.
package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/Rohan-Muslekar/knobs/internal/auth"
	"github.com/Rohan-Muslekar/knobs/internal/delivery"
	"github.com/Rohan-Muslekar/knobs/internal/store"
	"github.com/Rohan-Muslekar/knobs/web"
)

// Deps carries everything the HTTP layer needs.
type Deps struct {
	Repo *store.Repo
	Auth *auth.Authenticator

	// Hub fans out fresh Snapshots to GET /v1/stream subscribers. Required
	// for handleStream; nil here would panic on the first stream request,
	// which is acceptable since every real caller (app.Run) always
	// constructs one.
	Hub *delivery.Hub

	// StreamHeartbeat overrides handleStream's SSE heartbeat interval.
	// Zero means "use the production default" (see stream_handler.go) —
	// tests set this to a short duration so they don't wait tens of
	// seconds to observe a heartbeat.
	StreamHeartbeat time.Duration

	// Shutdown is closed to tell every open handleStream connection to
	// return promptly instead of waiting on the next Hub publish or
	// heartbeat. app.Run closes this before calling srv.Shutdown, so
	// active SSE connections go idle up front rather than being the thing
	// srv.Shutdown's timeout has to forcibly wait out (srv.Shutdown itself
	// only ever waits out already-idle connections). A nil channel (the
	// zero value, e.g. in tests that don't exercise shutdown) simply never
	// fires — handleStream's select treats that case like any other never-
	// ready channel.
	Shutdown <-chan struct{}

	// LoginLimiter throttles POST /v1/auth/login per client IP. Nil here
	// (the zero value — every Deps literal outside this package, since the
	// type is unexported) is filled in by NewRouter with the production
	// default (loginMaxAttempts per loginWindow). A test within package
	// api can still construct its own via newLoginLimiter to exercise a
	// tighter budget or an injected clock.
	LoginLimiter *loginLimiter

	// TrustProxy tells clientIP whether to honor X-Forwarded-For (set from
	// config.Config.TrustProxy by app.Run). Defaults to false — the zero
	// value — in every test and any Deps literal that doesn't set it, so
	// XFF is ignored unless the operator has explicitly opted in. See
	// clientIP's doc comment in ratelimit.go for why that default matters.
	TrustProxy bool
}

// Production defaults for the login rate limiter. This is a per-instance,
// in-memory budget — fine for a single Knobs instance, but a multi-instance
// deployment would need a shared store (see loginLimiter's doc comment).
const (
	loginMaxAttempts = 10
	loginWindow      = 15 * time.Minute
)

// NewRouter wires all routes and returns the root handler.
func NewRouter(deps Deps) chi.Router {
	if deps.LoginLimiter == nil {
		deps.LoginLimiter = newLoginLimiter(loginMaxAttempts, loginWindow)
	}

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
			r.Get("/stream", deps.handleStream)
			r.Get("/schema", deps.handleSchemaDelivery)
		})
	})

	// SPA catch-all mounted last; explicit API routes above take precedence.
	assets := web.Assets()
	r.NotFound(spaHandler(assets))
	r.Get("/", spaHandler(assets))

	return r
}
