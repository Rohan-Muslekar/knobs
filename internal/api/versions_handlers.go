package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Rohan-Muslekar/knobs/internal/authz"
	"github.com/Rohan-Muslekar/knobs/internal/delivery"
	"github.com/Rohan-Muslekar/knobs/internal/store"
)

func (d Deps) handleListVersions(w http.ResponseWriter, r *http.Request) {
	envID, err := uuid.Parse(chi.URLParam(r, "envID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid environment id")
		return
	}
	uid, _ := currentUserID(r.Context())
	if _, _, err := d.authorizeEnv(r.Context(), d.Repo.Pool(), uid, envID, authz.RoleViewer); writeAuthzErr(w, err) {
		return
	}

	versions, err := d.Repo.ListVersions(r.Context(), d.Repo.Pool(), envID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not list versions")
		return
	}
	views := make([]map[string]any, 0, len(versions))
	for _, v := range versions {
		views = append(views, versionMetaView(v))
	}
	writeJSON(w, http.StatusOK, views)
}

type rollbackRequest struct {
	Version *int `json:"version"`
}

func (d Deps) handleRollback(w http.ResponseWriter, r *http.Request) {
	envID, err := uuid.Parse(chi.URLParam(r, "envID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid environment id")
		return
	}

	var req rollbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.Version == nil {
		writeErr(w, http.StatusUnprocessableEntity, "version required")
		return
	}

	// requireUser has already run on this route, so the id is always present;
	// the ok is discarded rather than checked again.
	uid, _ := currentUserID(r.Context())
	env, _, err := d.authorizeEnv(r.Context(), d.Repo.Pool(), uid, envID, authz.RoleEditor)
	if writeAuthzErr(w, err) {
		return
	}
	target := *req.Version
	var targetVersion store.ConfigVersion
	err = d.Repo.WithTx(r.Context(), func(tx pgxTx) error {
		var e error
		targetVersion, e = d.Repo.VersionByNumber(r.Context(), tx, env.ID, target)
		if e != nil {
			return e
		}
		revision, e := d.Repo.SetCurrentVersion(r.Context(), tx, env.ID, targetVersion.ID)
		if e != nil {
			return e
		}
		if e := d.Repo.RecordAudit(r.Context(), tx, env.ProjectID, uid, "values.rollback", env.ID.String(),
			map[string]any{"to": target}); e != nil {
			return e
		}
		// Delivered on COMMIT, same as the value-save path's notify: a
		// rollback that fails to commit never notifies subscribers. Rollback
		// still bumps delivery_revision via SetCurrentVersion — that's the
		// whole point of the revision axis: the config version can move
		// backwards on rollback, but the delivery revision never does, so
		// this notify carries the revision, not the (possibly lower) target
		// version.
		_, e = tx.Exec(r.Context(), "SELECT pg_notify($1, $2)", delivery.NotifyChannel,
			fmt.Sprintf("%s:%d", env.ID, revision))
		return e
	})
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "unknown version")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not roll back")
		return
	}
	writeJSON(w, http.StatusOK, valuesView(targetVersion))
}

func versionMetaView(v store.ConfigVersionMeta) map[string]any {
	var createdBy any
	if v.CreatedBy != nil {
		createdBy = *v.CreatedBy
	}
	return map[string]any{
		"id":            v.ID,
		"version":       v.Version,
		"schemaVersion": v.SchemaVersion,
		"createdBy":     createdBy,
		"createdAt":     v.CreatedAt,
		"isCurrent":     v.IsCurrent,
	}
}
