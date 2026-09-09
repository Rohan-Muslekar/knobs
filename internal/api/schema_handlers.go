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

	// requireUser has already run on this route, so the id is always present;
	// the ok is discarded rather than checked again.
	uid, _ := currentUserID(r.Context())
	var cs store.ConfigSchema
	err = d.Repo.WithTx(r.Context(), func(tx pgxTx) error {
		// GetOrInitSchema then LockProjectSchema, in that order, in this same
		// tx: the row must exist before it can be locked. Holding the lock
		// for the rest of this closure serializes against a concurrent
		// handlePutValues on this project, so the change-safety read below
		// sees committed values and can't be raced by a value write that
		// commits in between the read and UpdateSchema.
		if _, e := d.Repo.GetOrInitSchema(r.Context(), tx, projectID); e != nil {
			return e
		}
		if _, e := d.Repo.LockProjectSchema(r.Context(), tx, projectID); e != nil {
			return e
		}

		// Change-safety: every environment's currently-live values must
		// still validate against the new schema. Nothing is persisted until
		// every environment clears this check.
		envs, e := d.Repo.ListEnvironments(r.Context(), tx, projectID)
		if e != nil {
			return e
		}
		for _, env := range envs {
			if env.CurrentVersionID == nil {
				continue
			}
			cv, e := d.Repo.CurrentVersion(r.Context(), tx, env.ID)
			if errors.Is(e, store.ErrNotFound) {
				continue
			}
			if e != nil {
				return e
			}
			if e := schema.ValidateValues(compiled, cv.Values); e != nil {
				return &changeSafetyConflict{
					message:     "new schema is incompatible with existing values: " + e.Error(),
					environment: env.Name,
				}
			}
		}

		var e2 error
		cs, e2 = d.Repo.UpdateSchema(r.Context(), tx, projectID, def, uid)
		if e2 != nil {
			return e2
		}
		return d.Repo.RecordAudit(r.Context(), tx, projectID, uid, "schema.update", projectID.String(),
			map[string]any{"schemaVersion": cs.SchemaVersion})
	})
	if err != nil {
		// changeSafetyConflict means the tx rolled back because the new
		// schema is incompatible with some environment's live values — a
		// client error (409), not a server fault.
		var conflict *changeSafetyConflict
		if errors.As(err, &conflict) {
			writeJSON(w, http.StatusConflict, map[string]string{
				"error":       conflict.message,
				"environment": conflict.environment,
			})
			return
		}
		writeErr(w, http.StatusInternalServerError, "could not update schema")
		return
	}
	writeJSON(w, http.StatusOK, schemaView(cs))
}

// changeSafetyConflict wraps a change-safety failure surfaced from inside a
// WithTx closure, so the handler can tell "the tx rolled back because the
// new schema breaks an environment's live values" (409, a client error)
// apart from any other tx failure (500, a server error) once WithTx has
// returned.
type changeSafetyConflict struct {
	message     string
	environment string
}

func (c *changeSafetyConflict) Error() string { return c.message }

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
