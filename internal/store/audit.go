package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// RecordAudit appends an audit entry. Pass the same DBTX (tx) as the change it
// accompanies so the audit row commits atomically with it. actor/projectID may
// be uuid.Nil where not applicable.
func (r *Repo) RecordAudit(ctx context.Context, db DBTX, projectID, actor uuid.UUID, action, target string, diff any) error {
	var raw []byte
	if diff != nil {
		b, err := json.Marshal(diff)
		if err != nil {
			return err
		}
		raw = b
	}
	var pid, act *uuid.UUID
	if projectID != uuid.Nil {
		pid = &projectID
	}
	if actor != uuid.Nil {
		act = &actor
	}
	_, err := db.Exec(ctx,
		`INSERT INTO audit_log (project_id, actor, action, target, diff)
		 VALUES ($1, $2, $3, $4, $5)`,
		pid, act, action, target, raw)
	return err
}

// AuditEntry is a single row read back from audit_log.
type AuditEntry struct {
	ID        uuid.UUID
	ProjectID *uuid.UUID
	Actor     *uuid.UUID
	Action    string
	Target    string
	Diff      json.RawMessage
	At        time.Time
}

// ListAudit returns the most recent audit_log entries for projectID, newest
// first, bounded by limit. The caller is responsible for resolving limit to
// a sane bound (default/cap/floor); this method applies whatever value it is
// given.
func (r *Repo) ListAudit(ctx context.Context, db DBTX, projectID uuid.UUID, limit int) ([]AuditEntry, error) {
	rows, err := db.Query(ctx,
		`SELECT id, project_id, actor, action, target, diff, at
		 FROM audit_log WHERE project_id = $1 ORDER BY at DESC LIMIT $2`,
		projectID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEntry
	for rows.Next() {
		var e AuditEntry
		var raw []byte
		if err := rows.Scan(&e.ID, &e.ProjectID, &e.Actor, &e.Action, &e.Target, &raw, &e.At); err != nil {
			return nil, err
		}
		if raw != nil {
			e.Diff = json.RawMessage(raw)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
