package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Rohan-Muslekar/knobs/internal/apikey"
	"github.com/Rohan-Muslekar/knobs/internal/authz"
	"github.com/Rohan-Muslekar/knobs/internal/store"
)

type createApiKeyRequest struct {
	Name string `json:"name"`
}

// handleCreateApiKey generates a new API key for an environment. The
// plaintext is returned in this response only — it is never persisted, and
// never appears again (not in list, not in the audit diff).
func (d Deps) handleCreateApiKey(w http.ResponseWriter, r *http.Request) {
	envID, err := uuid.Parse(chi.URLParam(r, "envID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid environment id")
		return
	}
	var req createApiKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.Name == "" {
		writeErr(w, http.StatusUnprocessableEntity, "name required")
		return
	}
	uid, _ := currentUserID(r.Context())
	env, _, err := d.authorizeEnv(r.Context(), d.Repo.Pool(), uid, envID, authz.RoleAdmin)
	if writeAuthzErr(w, err) {
		return
	}
	plaintext, hash, err := apikey.Generate()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not generate key")
		return
	}
	var k store.ApiKey
	err = d.Repo.WithTx(r.Context(), func(tx pgxTx) error {
		var e error
		k, e = d.Repo.CreateApiKey(r.Context(), tx, envID, env.ProjectID, req.Name, hash)
		if e != nil {
			return e
		}
		// The diff carries only the key id and name — never the plaintext or
		// the hash, so a leaked audit_log row can't be used to authenticate.
		return d.Repo.RecordAudit(r.Context(), tx, env.ProjectID, uid, "apikey.create", k.ID.String(),
			map[string]string{"keyId": k.ID.String(), "name": k.Name})
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not create api key")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":        k.ID,
		"name":      k.Name,
		"key":       plaintext,
		"createdAt": k.CreatedAt,
	})
}

func (d Deps) handleListApiKeys(w http.ResponseWriter, r *http.Request) {
	envID, err := uuid.Parse(chi.URLParam(r, "envID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid environment id")
		return
	}
	uid, _ := currentUserID(r.Context())
	if _, _, err := d.authorizeEnv(r.Context(), d.Repo.Pool(), uid, envID, authz.RoleViewer); writeAuthzErr(w, err) {
		return
	}
	keys, err := d.Repo.ListApiKeys(r.Context(), d.Repo.Pool(), envID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not list api keys")
		return
	}
	views := make([]map[string]any, 0, len(keys))
	for _, k := range keys {
		views = append(views, apiKeyView(k))
	}
	writeJSON(w, http.StatusOK, views)
}

func (d Deps) handleRevokeApiKey(w http.ResponseWriter, r *http.Request) {
	envID, err := uuid.Parse(chi.URLParam(r, "envID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid environment id")
		return
	}
	keyID, err := uuid.Parse(chi.URLParam(r, "keyID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid key id")
		return
	}
	// requireUser has already run on this route, so the id is always present;
	// the ok is discarded rather than checked again.
	uid, _ := currentUserID(r.Context())
	env, _, err := d.authorizeEnv(r.Context(), d.Repo.Pool(), uid, envID, authz.RoleAdmin)
	if writeAuthzErr(w, err) {
		return
	}
	err = d.Repo.WithTx(r.Context(), func(tx pgxTx) error {
		if e := d.Repo.RevokeApiKey(r.Context(), tx, keyID, envID); e != nil {
			return e
		}
		return d.Repo.RecordAudit(r.Context(), tx, env.ProjectID, uid, "apikey.revoke", keyID.String(),
			map[string]string{"keyId": keyID.String()})
	})
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "api key not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not revoke api key")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func apiKeyView(k store.ApiKey) map[string]any {
	var lastUsedAt any
	if k.LastUsedAt != nil {
		lastUsedAt = *k.LastUsedAt
	}
	return map[string]any{
		"id":         k.ID,
		"name":       k.Name,
		"scope":      k.Scope,
		"createdAt":  k.CreatedAt,
		"lastUsedAt": lastUsedAt,
	}
}
