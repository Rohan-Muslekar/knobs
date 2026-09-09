package api

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/Rohan-Muslekar/knobs/internal/auth"
)

type ctxKey int

const userIDKey ctxKey = iota

// requireUser verifies the session cookie and injects the user id. It responds
// 401 JSON when the cookie is missing or invalid.
func requireUser(deps Deps) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, err := r.Cookie(auth.CookieName())
			if err != nil {
				writeErr(w, http.StatusUnauthorized, "authentication required")
				return
			}
			uid, err := deps.Auth.Verify(c.Value)
			if err != nil {
				writeErr(w, http.StatusUnauthorized, "invalid session")
				return
			}
			ctx := context.WithValue(r.Context(), userIDKey, uid)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func currentUserID(ctx context.Context) (uuid.UUID, bool) {
	uid, ok := ctx.Value(userIDKey).(uuid.UUID)
	return uid, ok
}
