package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Rohan-Muslekar/knobs/internal/store"
)

const (
	defaultAuditLimit = 100
	maxAuditLimit     = 500
	minAuditLimit     = 1
)

func (d Deps) handleListAudit(w http.ResponseWriter, r *http.Request) {
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

	limit := parseAuditLimit(r.URL.Query().Get("limit"))

	entries, err := d.Repo.ListAudit(r.Context(), d.Repo.Pool(), projectID, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not list audit log")
		return
	}
	views := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		views = append(views, auditEntryView(e))
	}
	writeJSON(w, http.StatusOK, views)
}

// parseAuditLimit resolves the ?limit= query param to a bounded value: an
// empty or unparseable value falls back to the default, and any parsed
// value is clamped to [minAuditLimit, maxAuditLimit].
func parseAuditLimit(raw string) int {
	if raw == "" {
		return defaultAuditLimit
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return defaultAuditLimit
	}
	if n < minAuditLimit {
		return minAuditLimit
	}
	if n > maxAuditLimit {
		return maxAuditLimit
	}
	return n
}

func auditEntryView(e store.AuditEntry) map[string]any {
	var actor any
	if e.Actor != nil {
		actor = *e.Actor
	}
	var diff any
	if e.Diff != nil {
		diff = e.Diff
	}
	return map[string]any{
		"id":     e.ID,
		"actor":  actor,
		"action": e.Action,
		"target": e.Target,
		"diff":   diff,
		"at":     e.At,
	}
}
