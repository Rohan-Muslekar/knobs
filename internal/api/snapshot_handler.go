package api

import (
	"errors"
	"net/http"

	"github.com/Rohan-Muslekar/knobs/internal/delivery"
	"github.com/Rohan-Muslekar/knobs/internal/store"
)

// handleSnapshot serves GET /v1/snapshot. It sits behind apiKeyGuard, not
// requireUser: the environment comes from the presented key's scope in
// context, never from a query parameter, so there is no `?env=` to trust or
// distrust — a key can only ever produce its own environment's snapshot.
func (d Deps) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	envID, ok := apiKeyEnvID(r.Context())
	if !ok {
		writeErr(w, http.StatusUnauthorized, "authentication required")
		return
	}

	env, err := d.Repo.EnvironmentByID(r.Context(), d.Repo.Pool(), envID)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "environment not found")
		return
	} else if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not load environment")
		return
	}

	cv, err := d.Repo.CurrentVersion(r.Context(), d.Repo.Pool(), envID)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "no values set")
		return
	} else if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not load values")
		return
	}

	cs, err := d.Repo.GetOrInitSchema(r.Context(), d.Repo.Pool(), env.ProjectID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not load schema")
		return
	}

	writeJSON(w, http.StatusOK, delivery.BuildSnapshot(cv, cs.Definition))
}
