package api

import (
	"errors"
	"net/http"

	"github.com/Rohan-Muslekar/knobs/internal/delivery"
	"github.com/Rohan-Muslekar/knobs/internal/store"
)

// handleSchemaDelivery serves GET /v1/schema. Like handleSnapshot, it sits
// behind apiKeyGuard, not requireUser: the environment comes from the
// presented key's scope in context, so a key can only ever fetch its own
// environment's project schema — codegen and SDK drift-detection both need
// this without ever seeing another project's field definitions.
func (d Deps) handleSchemaDelivery(w http.ResponseWriter, r *http.Request) {
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

	cs, err := d.Repo.GetOrInitSchema(r.Context(), d.Repo.Pool(), env.ProjectID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not load schema")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"definition":    cs.Definition,
		"schemaVersion": cs.SchemaVersion,
		"schemaHash":    delivery.SchemaHash(cs.Definition),
	})
}
