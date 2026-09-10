package api

import (
	"encoding/json"
	"net/http"

	"github.com/Rohan-Muslekar/knobs/internal/auth"
)

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (d Deps) handleLogin(w http.ResponseWriter, r *http.Request) {
	key := clientIP(r, d.TrustProxy)
	if d.LoginLimiter != nil && !d.LoginLimiter.underLimit(key) {
		writeErr(w, http.StatusTooManyRequests, "too many login attempts, try again later")
		return
	}

	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	u, err := d.Repo.UserByEmail(r.Context(), d.Repo.Pool(), req.Email)
	hash := u.PasswordHash
	if err != nil {
		// Check against a dummy hash so a missing user takes the same time
		// as a wrong password, defeating user enumeration via timing.
		hash = auth.DummyHash
	}
	ok := d.Auth.Check(hash, req.Password)
	if err != nil || !ok {
		// Same response whether the user is missing or the password is wrong.
		// Count it against the limiter — a successful login doesn't consume
		// this client's budget.
		if d.LoginLimiter != nil {
			d.LoginLimiter.allow(key)
		}
		writeErr(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	tok, err := d.Auth.Issue(u.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not issue session")
		return
	}
	d.Auth.SetCookie(w, tok)
	writeJSON(w, http.StatusOK, map[string]any{"id": u.ID, "email": u.Email})
}

func (d Deps) handleLogout(w http.ResponseWriter, _ *http.Request) {
	d.Auth.ClearCookie(w)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (d Deps) handleMe(w http.ResponseWriter, r *http.Request) {
	uid, ok := currentUserID(r.Context())
	if !ok {
		writeErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	u, err := d.Repo.UserByID(r.Context(), d.Repo.Pool(), uid)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "unknown user")
		return
	}
	orgs, err := d.Repo.ListOrganizationsForUser(r.Context(), d.Repo.Pool(), uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not list organizations")
		return
	}
	orgViews := make([]map[string]any, 0, len(orgs))
	for _, o := range orgs {
		orgViews = append(orgViews, map[string]any{
			"id": o.ID, "name": o.Name, "slug": o.Slug, "role": o.Role,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": u.ID, "email": u.Email, "organizations": orgViews})
}
