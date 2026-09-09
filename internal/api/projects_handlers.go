package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Rohan-Muslekar/knobs/internal/store"
)

var slugRe = regexp.MustCompile(`^[a-z0-9-]+$`)

type createProjectRequest struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

func (d Deps) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var req createProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.Name == "" || !slugRe.MatchString(req.Slug) {
		writeErr(w, http.StatusUnprocessableEntity, "name required and slug must match ^[a-z0-9-]+$")
		return
	}
	// requireUser has already run on this route, so the id is always present;
	// the ok is discarded rather than checked again.
	uid, _ := currentUserID(r.Context())
	var p store.Project
	err := d.Repo.WithTx(r.Context(), func(tx pgxTx) error {
		var e error
		p, e = d.Repo.CreateProject(r.Context(), tx, req.Name, req.Slug)
		if e != nil {
			return e
		}
		return d.Repo.RecordAudit(r.Context(), tx, p.ID, uid, "project.create", p.ID.String(),
			map[string]string{"name": p.Name, "slug": p.Slug})
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeErr(w, http.StatusConflict, "slug already exists")
			return
		}
		writeErr(w, http.StatusInternalServerError, "could not create project")
		return
	}
	writeJSON(w, http.StatusCreated, projectView(p))
}

func (d Deps) handleListProjects(w http.ResponseWriter, r *http.Request) {
	ps, err := d.Repo.ListProjects(r.Context(), d.Repo.Pool())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not list projects")
		return
	}
	views := make([]map[string]any, 0, len(ps))
	for _, p := range ps {
		views = append(views, projectView(p))
	}
	writeJSON(w, http.StatusOK, views)
}

func (d Deps) handleGetProject(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "projectID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid project id")
		return
	}
	p, err := d.Repo.ProjectByID(r.Context(), d.Repo.Pool(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "project not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not load project")
		return
	}
	writeJSON(w, http.StatusOK, projectView(p))
}

type patchProjectRequest struct {
	Name string `json:"name"`
}

func (d Deps) handlePatchProject(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "projectID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid project id")
		return
	}
	var req patchProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeErr(w, http.StatusUnprocessableEntity, "name required")
		return
	}
	// requireUser has already run on this route, so the id is always present;
	// the ok is discarded rather than checked again.
	uid, _ := currentUserID(r.Context())
	var p store.Project
	err = d.Repo.WithTx(r.Context(), func(tx pgxTx) error {
		var e error
		p, e = d.Repo.UpdateProjectName(r.Context(), tx, id, req.Name)
		if e != nil {
			return e
		}
		return d.Repo.RecordAudit(r.Context(), tx, id, uid, "project.rename", id.String(),
			map[string]string{"name": req.Name})
	})
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "project not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not update project")
		return
	}
	writeJSON(w, http.StatusOK, projectView(p))
}

func projectView(p store.Project) map[string]any {
	return map[string]any{"id": p.ID, "name": p.Name, "slug": p.Slug, "createdAt": p.CreatedAt}
}
