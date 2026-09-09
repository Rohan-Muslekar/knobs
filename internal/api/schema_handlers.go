package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Rohan-Muslekar/knobs/internal/schema"
	"github.com/Rohan-Muslekar/knobs/internal/store"
)

func (d Deps) handleGetSchema(w http.ResponseWriter, r *http.Request) {
	projectID, err := uuid.Parse(chi.URLParam(r, "projectID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid project id")
		return
	}
	if _, err := d.Repo.ProjectByID(r.Context(), d.Repo.Pool(), projectID); errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "project not found")
		return
	} else if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not load project")
		return
	}
	cs, err := d.Repo.GetOrInitSchema(r.Context(), d.Repo.Pool(), projectID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not load schema")
		return
	}
	writeJSON(w, http.StatusOK, schemaView(cs))
}

type putSchemaRequest struct {
	Fields []schema.Field `json:"fields"`
}

func (d Deps) handlePutSchema(w http.ResponseWriter, r *http.Request) {
	projectID, err := uuid.Parse(chi.URLParam(r, "projectID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid project id")
		return
	}
	if _, err := d.Repo.ProjectByID(r.Context(), d.Repo.Pool(), projectID); errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "project not found")
		return
	} else if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not load project")
		return
	}

	var req putSchemaRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	def := schema.Definition{Fields: req.Fields}
	if err := schema.ValidateDefinition(def); err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	compiled, err := schema.Compile(def)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, "schema does not compile: "+err.Error())
		return
	}

	// Change-safety: every environment's currently-live values must still
	// validate against the new schema. Nothing is persisted until every
	// environment clears this check.
	envs, err := d.Repo.ListEnvironments(r.Context(), d.Repo.Pool(), projectID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not list environments")
		return
	}
	for _, env := range envs {
		if env.CurrentVersionID == nil {
			continue
		}
		cv, err := d.Repo.CurrentVersion(r.Context(), d.Repo.Pool(), env.ID)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "could not load current values")
			return
		}
		if err := schema.ValidateValues(compiled, cv.Values); err != nil {
			writeJSON(w, http.StatusConflict, map[string]string{
				"error":       "new schema is incompatible with existing values: " + err.Error(),
				"environment": env.Name,
			})
			return
		}
	}

	// requireUser has already run on this route, so the id is always present;
	// the ok is discarded rather than checked again.
	uid, _ := currentUserID(r.Context())
	var cs store.ConfigSchema
	err = d.Repo.WithTx(r.Context(), func(tx pgxTx) error {
		var e error
		cs, e = d.Repo.UpdateSchema(r.Context(), tx, projectID, def, uid)
		if e != nil {
			return e
		}
		return d.Repo.RecordAudit(r.Context(), tx, projectID, uid, "schema.update", projectID.String(),
			map[string]any{"schemaVersion": cs.SchemaVersion})
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not update schema")
		return
	}
	writeJSON(w, http.StatusOK, schemaView(cs))
}

func schemaView(cs store.ConfigSchema) map[string]any {
	fields := cs.Definition.Fields
	if fields == nil {
		fields = []schema.Field{}
	}
	return map[string]any{
		"definition":    map[string]any{"fields": fields},
		"schemaVersion": cs.SchemaVersion,
	}
}
