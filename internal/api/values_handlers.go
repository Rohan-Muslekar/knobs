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

	// requireUser has already run on this route, so the id is always present;
	// the ok is discarded rather than checked again.
	uid, _ := currentUserID(r.Context())
	var newVersion store.ConfigVersion
	err = d.Repo.WithTx(r.Context(), func(tx pgxTx) error {
		// The schema read and the validation against it must happen under
		// the same row lock a concurrent handlePutSchema takes, and the read
		// itself must happen as part of taking that lock, not before it.
		// GetOrInitSchema only ensures the row exists (a plain, unlocked
		// read); LockProjectSchema then does the FOR UPDATE read that is
		// actually authoritative. Using GetOrInitSchema's result here
		// instead would leave a window between that read and the lock where
		// a concurrent schema tightening could commit, and this handler
		// would validate against the stale, already-superseded schema
		// despite "holding the lock" for everything after.
		if _, e := d.Repo.GetOrInitSchema(r.Context(), tx, env.ProjectID); e != nil {
			return e
		}
		currentSchema, e := d.Repo.LockProjectSchema(r.Context(), tx, env.ProjectID)
		if e != nil {
			return e
		}
		compiled, e := schema.Compile(currentSchema.Definition)
		if e != nil {
			return &validationFailure{err: fmt.Errorf("schema does not compile: %w", e)}
		}
		if e := schema.ValidateValues(compiled, req.Values); e != nil {
			return &validationFailure{err: e}
		}

		n, e := d.Repo.NextVersionNumber(r.Context(), tx, env.ID)
		if e != nil {
			return e
		}
		newVersion, e = d.Repo.InsertVersion(r.Context(), tx, env.ID, n, currentSchema.SchemaVersion, req.Values, uid)
		if e != nil {
			return e
		}
		revision, e := d.Repo.SetCurrentVersion(r.Context(), tx, env.ID, newVersion.ID)
		if e != nil {
			return e
		}
		if e := d.Repo.RecordAudit(r.Context(), tx, env.ProjectID, uid, "values.update", env.ID.String(),
			map[string]any{"version": n}); e != nil {
			return e
		}
		// Delivered on COMMIT (Postgres defers NOTIFY delivery until the
		// transaction commits), so a rolled-back write never fires this —
		// the Listener only ever sees notifications for versions that are
		// actually live. The second field is the monotonic delivery
		// revision, not the config version: it's what the Listener/SDK
		// dedupe and gate `since` on, since (unlike version) it never moves
		// backwards on rollback.
		_, e = tx.Exec(r.Context(), "SELECT pg_notify($1, $2)", delivery.NotifyChannel,
			fmt.Sprintf("%s:%d", env.ID, revision))
		return e
	})
	if err != nil {
		// validationFailure means the tx rolled back because req.Values does
		// not satisfy the (lock-consistent) current schema — a client error,
		// not a server fault.
		var vf *validationFailure
		if errors.As(err, &vf) {
			writeErr(w, http.StatusUnprocessableEntity, vf.Error())
			return
		}
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

// validationFailure wraps a schema-validation error surfaced from inside a
// WithTx closure, so the handler can tell "the tx rolled back because the
// input is invalid" (422, a client error) apart from any other tx failure
// (500, a server error) once WithTx has returned.
type validationFailure struct{ err error }

func (v *validationFailure) Error() string { return v.err.Error() }
func (v *validationFailure) Unwrap() error { return v.err }

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
