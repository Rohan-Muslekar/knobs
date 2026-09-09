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
	writeJSON(w, http.StatusOK, map[string]any{"id": u.ID, "email": u.Email})
}
