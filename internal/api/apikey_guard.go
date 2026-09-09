package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/Rohan-Muslekar/knobs/internal/apikey"
	"github.com/Rohan-Muslekar/knobs/internal/store"
)

// apiKeyCtxKey is its own type, distinct from middleware.go's ctxKey, so the
// environment id a delivery request carries can never collide with — or be
// mistaken for — the session user id requireUser sets. Even if the
// underlying int values matched, context.Value keys compare by (type,
// value), so the two guards can't observe each other's data.
type apiKeyCtxKey int

const envIDKey apiKeyCtxKey = iota

// apiKeyGuard verifies a delivery request's `Authorization: Bearer <key>`
// header and injects the key's environment id into the request context.
// This is the auth path for /v1/snapshot and /v1/stream — wholly separate
// from requireUser's session cookie. A missing header, a header that isn't
// "Bearer <token>", or a token that doesn't hash to a known key all respond
// 401, so a valid session cookie alone never satisfies delivery auth.
func apiKeyGuard(deps Deps) func(http.Handler) http.Handler {
	const prefix = "Bearer "
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := r.Header.Get("Authorization")
			if !strings.HasPrefix(h, prefix) {
				writeErr(w, http.StatusUnauthorized, "authentication required")
				return
			}
			plaintext := strings.TrimPrefix(h, prefix)
			if plaintext == "" {
				writeErr(w, http.StatusUnauthorized, "authentication required")
				return
			}
			k, err := deps.Repo.ApiKeyByHash(r.Context(), deps.Repo.Pool(), apikey.Hash(plaintext))
			if errors.Is(err, store.ErrNotFound) {
				writeErr(w, http.StatusUnauthorized, "invalid api key")
				return
			}
			if err != nil {
				writeErr(w, http.StatusInternalServerError, "could not verify api key")
				return
			}
			// Best-effort: the touch is bookkeeping, not the point of the
			// request, so its failure must never fail or slow the caller down.
			_ = deps.Repo.TouchApiKey(r.Context(), deps.Repo.Pool(), k.ID)

			ctx := context.WithValue(r.Context(), envIDKey, k.EnvironmentID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// apiKeyEnvID returns the environment id apiKeyGuard put on the request
// context, i.e. the environment the presented key is scoped to.
func apiKeyEnvID(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(envIDKey).(uuid.UUID)
	return id, ok
}
