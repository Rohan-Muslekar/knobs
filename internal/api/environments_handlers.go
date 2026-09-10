package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Rohan-Muslekar/knobs/internal/authz"
	"github.com/Rohan-Muslekar/knobs/internal/store"
)

type createEnvironmentRequest struct {
	Name string `json:"name"`
}

func (d Deps) handleCreateEnvironment(w http.ResponseWriter, r *http.Request) {
	projectID, err := uuid.Parse(chi.URLParam(r, "projectID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid project id")
		return
	}
	var req createEnvironmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.Name == "" || !slugRe.MatchString(req.Name) {
		writeErr(w, http.StatusUnprocessableEntity, "name required and must match ^[a-z0-9-]+$")
		return
	}
	// requireUser has already run on this route, so the id is always present;
	// the ok is discarded rather than checked again.
	uid, _ := currentUserID(r.Context())
	if _, _, err := d.authorizeProject(r.Context(), d.Repo.Pool(), uid, projectID, authz.RoleAdmin); writeAuthzErr(w, err) {
		return
	}
	var e store.Environment
	err = d.Repo.WithTx(r.Context(), func(tx pgxTx) error {
		var e2 error
		e, e2 = d.Repo.CreateEnvironment(r.Context(), tx, projectID, req.Name)
		if e2 != nil {
			return e2
		}
		return d.Repo.RecordAudit(r.Context(), tx, projectID, uid, "environment.create", e.ID.String(),
			map[string]string{"name": e.Name})
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeErr(w, http.StatusConflict, "environment name already exists")
			return
		}
		writeErr(w, http.StatusInternalServerError, "could not create environment")
		return
	}
	writeJSON(w, http.StatusCreated, environmentView(e))
}

func (d Deps) handleListEnvironments(w http.ResponseWriter, r *http.Request) {
	projectID, err := uuid.Parse(chi.URLParam(r, "projectID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid project id")
		return
	}
	uid, _ := currentUserID(r.Context())
	if _, _, err := d.authorizeProject(r.Context(), d.Repo.Pool(), uid, projectID, authz.RoleViewer); writeAuthzErr(w, err) {
		return
	}
	es, err := d.Repo.ListEnvironments(r.Context(), d.Repo.Pool(), projectID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not list environments")
		return
	}
	views := make([]map[string]any, 0, len(es))
	for _, e := range es {
		views = append(views, environmentView(e))
	}
	writeJSON(w, http.StatusOK, views)
}

func (d Deps) handleGetEnvironment(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "envID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid environment id")
		return
	}
	uid, _ := currentUserID(r.Context())
	e, _, err := d.authorizeEnv(r.Context(), d.Repo.Pool(), uid, id, authz.RoleViewer)
	if writeAuthzErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, environmentView(e))
}

func environmentView(e store.Environment) map[string]any {
	var currentVersionID any
	if e.CurrentVersionID != nil {
		currentVersionID = *e.CurrentVersionID
	}
	return map[string]any{
		"id":               e.ID,
		"projectId":        e.ProjectID,
		"name":             e.Name,
		"currentVersionId": currentVersionID,
		"createdAt":        e.CreatedAt,
	}
}
