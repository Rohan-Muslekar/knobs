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
//
// NOTE: only ConfigVersion and CurrentVersion live here for now. The
// remaining version-store methods (NextVersionNumber, InsertVersion,
// SetCurrentVersion, VersionByNumber) belong to a later task and will
// extend this same file — do not add them here.
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
