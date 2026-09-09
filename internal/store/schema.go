package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Rohan-Muslekar/knobs/internal/schema"
)

// ConfigSchema is a project's typed field definition plus its version.
type ConfigSchema struct {
	ProjectID     uuid.UUID
	Definition    schema.Definition
	SchemaVersion int
	UpdatedAt     time.Time
}

// GetOrInitSchema returns the project's config schema row, creating a
// default empty definition (`{"fields":[]}`, schema_version 1) on first
// access.
func (r *Repo) GetOrInitSchema(ctx context.Context, db DBTX, projectID uuid.UUID) (ConfigSchema, error) {
	if _, err := db.Exec(ctx,
		`INSERT INTO config_schema (project_id) VALUES ($1) ON CONFLICT (project_id) DO NOTHING`,
		projectID,
	); err != nil {
		return ConfigSchema{}, err
	}
	return r.schemaByProjectID(ctx, db, projectID)
}

// UpdateSchema stores a new definition for the project, bumping
// schema_version. If the project has no config_schema row yet, this creates
// one at version 1 rather than requiring a prior GetOrInitSchema call.
func (r *Repo) UpdateSchema(ctx context.Context, db DBTX, projectID uuid.UUID, def schema.Definition, updatedBy uuid.UUID) (ConfigSchema, error) {
	raw, err := json.Marshal(def)
	if err != nil {
		return ConfigSchema{}, err
	}
	var ub *uuid.UUID
	if updatedBy != uuid.Nil {
		ub = &updatedBy
	}
	var cs ConfigSchema
	var defRaw []byte
	err = db.QueryRow(ctx,
		`INSERT INTO config_schema (project_id, definition, schema_version, updated_by, updated_at)
		 VALUES ($1, $2, 1, $3, now())
		 ON CONFLICT (project_id) DO UPDATE
		 SET definition = EXCLUDED.definition,
		     schema_version = config_schema.schema_version + 1,
		     updated_by = EXCLUDED.updated_by,
		     updated_at = now()
		 RETURNING project_id, definition, schema_version, updated_at`,
		projectID, raw, ub,
	).Scan(&cs.ProjectID, &defRaw, &cs.SchemaVersion, &cs.UpdatedAt)
	if err != nil {
		return ConfigSchema{}, err
	}
	if err := json.Unmarshal(defRaw, &cs.Definition); err != nil {
		return ConfigSchema{}, err
	}
	return cs, nil
}

// LockProjectSchema takes a FOR UPDATE row lock on the project's
// config_schema row for the lifetime of tx, serializing it against any other
// transaction locking the same row. Callers must call GetOrInitSchema (or
// otherwise ensure the row exists) first in the same tx, since a missing row
// has nothing to lock. Used to make schema writes and value writes mutually
// exclusive per project, so a change-safety check and a value validation
// never interleave against each other.
func (r *Repo) LockProjectSchema(ctx context.Context, tx DBTX, projectID uuid.UUID) error {
	var discard int
	err := tx.QueryRow(ctx,
		`SELECT 1 FROM config_schema WHERE project_id = $1 FOR UPDATE`,
		projectID,
	).Scan(&discard)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func (r *Repo) schemaByProjectID(ctx context.Context, db DBTX, projectID uuid.UUID) (ConfigSchema, error) {
	var cs ConfigSchema
	var defRaw []byte
	err := db.QueryRow(ctx,
		`SELECT project_id, definition, schema_version, updated_at
		 FROM config_schema WHERE project_id = $1`,
		projectID,
	).Scan(&cs.ProjectID, &defRaw, &cs.SchemaVersion, &cs.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ConfigSchema{}, ErrNotFound
	}
	if err != nil {
		return ConfigSchema{}, err
	}
	if err := json.Unmarshal(defRaw, &cs.Definition); err != nil {
		return ConfigSchema{}, err
	}
	return cs, nil
}
