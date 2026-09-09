package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Rohan-Muslekar/knobs/internal/delivery"
	"github.com/Rohan-Muslekar/knobs/internal/schema"
	"github.com/Rohan-Muslekar/knobs/internal/store"
)

func (d Deps) handleGetValues(w http.ResponseWriter, r *http.Request) {
	envID, err := uuid.Parse(chi.URLParam(r, "envID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid environment id")
		return
	}
	if _, err := d.Repo.EnvironmentByID(r.Context(), d.Repo.Pool(), envID); errors.Is(err, store.ErrNotFound) {
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
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not load values")
		return
	}
	writeJSON(w, http.StatusOK, valuesView(cv))
}

type putValuesRequest struct {
	Values map[string]any `json:"values"`
}

func (d Deps) handlePutValues(w http.ResponseWriter, r *http.Request) {
	envID, err := uuid.Parse(chi.URLParam(r, "envID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid environment id")
		return
	}

	var req putValuesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
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

	currentSchema, err := d.Repo.GetOrInitSchema(r.Context(), d.Repo.Pool(), env.ProjectID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not load schema")
		return
	}
	compiled, err := schema.Compile(currentSchema.Definition)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "schema does not compile: "+err.Error())
		return
	}
	if err := schema.ValidateValues(compiled, req.Values); err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	// requireUser has already run on this route, so the id is always present;
	// the ok is discarded rather than checked again.
	uid, _ := currentUserID(r.Context())
	var newVersion store.ConfigVersion
	err = d.Repo.WithTx(r.Context(), func(tx pgxTx) error {
		n, e := d.Repo.NextVersionNumber(r.Context(), tx, env.ID)
		if e != nil {
			return e
		}
		newVersion, e = d.Repo.InsertVersion(r.Context(), tx, env.ID, n, currentSchema.SchemaVersion, req.Values, uid)
		if e != nil {
			return e
		}
		if e := d.Repo.SetCurrentVersion(r.Context(), tx, env.ID, newVersion.ID); e != nil {
			return e
		}
		if e := d.Repo.RecordAudit(r.Context(), tx, env.ProjectID, uid, "values.update", env.ID.String(),
			map[string]any{"version": n}); e != nil {
			return e
		}
		// Delivered on COMMIT (Postgres defers NOTIFY delivery until the
		// transaction commits), so a rolled-back write never fires this —
		// the Listener only ever sees notifications for versions that are
		// actually live.
		_, e = tx.Exec(r.Context(), "SELECT pg_notify($1, $2)", delivery.NotifyChannel,
			fmt.Sprintf("%s:%d", env.ID, n))
		return e
	})
	if err != nil {
		// Two concurrent writes can both read the same NextVersionNumber and
		// then race to insert it; UNIQUE(environment_id, version) rejects the
		// loser with a 23505, which we surface as a 409 so the client knows
		// to retry rather than treating it as a server fault.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeErr(w, http.StatusConflict, "version conflict, retry")
			return
		}
		writeErr(w, http.StatusInternalServerError, "could not update values")
		return
	}
	writeJSON(w, http.StatusOK, valuesView(newVersion))
}

func valuesView(cv store.ConfigVersion) map[string]any {
	values := cv.Values
	if values == nil {
		values = map[string]any{}
	}
	return map[string]any{
		"version":       cv.Version,
		"values":        values,
		"schemaVersion": cv.SchemaVersion,
	}
}
