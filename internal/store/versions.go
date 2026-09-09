package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ConfigVersion is a single immutable snapshot of an environment's config
// values, pinned to the schema_version it was validated against.
type ConfigVersion struct {
	ID            uuid.UUID
	EnvironmentID uuid.UUID
	Version       int
	SchemaVersion int
	Values        map[string]any
	CreatedBy     *uuid.UUID
	CreatedAt     time.Time
}

// CurrentVersion loads the config_version an environment currently points
// at, joining through environment.current_version_id. Returns ErrNotFound
// when the environment has no current version (current_version_id is nil).
func (r *Repo) CurrentVersion(ctx context.Context, db DBTX, envID uuid.UUID) (ConfigVersion, error) {
	var v ConfigVersion
	var raw []byte
	err := db.QueryRow(ctx,
		`SELECT cv.id, cv.environment_id, cv.version, cv.schema_version, cv.values, cv.created_by, cv.created_at
		 FROM environment e
		 JOIN config_version cv ON cv.id = e.current_version_id
		 WHERE e.id = $1`, envID,
	).Scan(&v.ID, &v.EnvironmentID, &v.Version, &v.SchemaVersion, &raw, &v.CreatedBy, &v.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ConfigVersion{}, ErrNotFound
	}
	if err != nil {
		return ConfigVersion{}, err
	}
	if err := json.Unmarshal(raw, &v.Values); err != nil {
		return ConfigVersion{}, err
	}
	return v, nil
}

// NextVersionNumber returns the version number a new config_version row for
// envID should take: the current max plus one, or 1 if the environment has
// no versions yet.
func (r *Repo) NextVersionNumber(ctx context.Context, tx DBTX, envID uuid.UUID) (int, error) {
	var n int
	err := tx.QueryRow(ctx,
		`SELECT COALESCE(MAX(version), 0) + 1 FROM config_version WHERE environment_id = $1`,
		envID,
	).Scan(&n)
	return n, err
}

// InsertVersion creates a new immutable config_version row for envID.
// Existing rows are never updated or deleted, so once written a version's
// values are permanent history. createdBy of uuid.Nil is stored as NULL.
func (r *Repo) InsertVersion(ctx context.Context, tx DBTX, envID uuid.UUID, version, schemaVersion int, values map[string]any, createdBy uuid.UUID) (ConfigVersion, error) {
	raw, err := json.Marshal(values)
	if err != nil {
		return ConfigVersion{}, err
	}
	var cb *uuid.UUID
	if createdBy != uuid.Nil {
		cb = &createdBy
	}
	var v ConfigVersion
	var vraw []byte
	err = tx.QueryRow(ctx,
		`INSERT INTO config_version (environment_id, version, values, schema_version, created_by)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, environment_id, version, schema_version, values, created_by, created_at`,
		envID, version, raw, schemaVersion, cb,
	).Scan(&v.ID, &v.EnvironmentID, &v.Version, &v.SchemaVersion, &vraw, &v.CreatedBy, &v.CreatedAt)
	if err != nil {
		return ConfigVersion{}, err
	}
	if err := json.Unmarshal(vraw, &v.Values); err != nil {
		return ConfigVersion{}, err
	}
	return v, nil
}

// SetCurrentVersion points envID's live config at versionID.
func (r *Repo) SetCurrentVersion(ctx context.Context, tx DBTX, envID, versionID uuid.UUID) error {
	_, err := tx.Exec(ctx, `UPDATE environment SET current_version_id = $2 WHERE id = $1`, envID, versionID)
	return err
}

// ConfigVersionMeta is a history-listing view of a config_version row: it
// carries everything ListVersions needs except the values blob, which is
// deliberately omitted (callers wanting values use VersionByNumber).
type ConfigVersionMeta struct {
	ID            uuid.UUID
	Version       int
	SchemaVersion int
	CreatedBy     *uuid.UUID
	CreatedAt     time.Time
	IsCurrent     bool
}

// ListVersions returns every version recorded for envID, most recent first.
// IsCurrent is set on whichever row's id matches the environment's
// current_version_id.
func (r *Repo) ListVersions(ctx context.Context, db DBTX, envID uuid.UUID) ([]ConfigVersionMeta, error) {
	rows, err := db.Query(ctx,
		`SELECT cv.id, cv.version, cv.schema_version, cv.created_by, cv.created_at,
		        cv.id = e.current_version_id AS is_current
		 FROM config_version cv
		 JOIN environment e ON e.id = cv.environment_id
		 WHERE cv.environment_id = $1
		 ORDER BY cv.version DESC`, envID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ConfigVersionMeta
	for rows.Next() {
		var m ConfigVersionMeta
		if err := rows.Scan(&m.ID, &m.Version, &m.SchemaVersion, &m.CreatedBy, &m.CreatedAt, &m.IsCurrent); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// VersionByNumber loads a specific historical version by its (environment,
// version) pair, regardless of whether it is still the environment's
// current version. Returns ErrNotFound when no such version exists.
func (r *Repo) VersionByNumber(ctx context.Context, db DBTX, envID uuid.UUID, version int) (ConfigVersion, error) {
	var v ConfigVersion
	var raw []byte
	err := db.QueryRow(ctx,
		`SELECT id, environment_id, version, schema_version, values, created_by, created_at
		 FROM config_version WHERE environment_id = $1 AND version = $2`,
		envID, version,
	).Scan(&v.ID, &v.EnvironmentID, &v.Version, &v.SchemaVersion, &raw, &v.CreatedBy, &v.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ConfigVersion{}, ErrNotFound
	}
	if err != nil {
		return ConfigVersion{}, err
	}
	if err := json.Unmarshal(raw, &v.Values); err != nil {
		return ConfigVersion{}, err
	}
	return v, nil
}
