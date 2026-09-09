package store

import (
	"context"
	"encoding/json"

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
