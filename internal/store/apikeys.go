package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ApiKey is a delivery credential scoped to a single environment.
//
// It intentionally has no Hash field: the hash is a write-only secret
// used to look up a presented key, and must never leak into an API
// response or log line via this struct.
type ApiKey struct {
	ID            uuid.UUID
	EnvironmentID uuid.UUID
	ProjectID     uuid.UUID
	Name          string
	Scope         string
	CreatedAt     time.Time
	LastUsedAt    *time.Time
}

func (r *Repo) CreateApiKey(ctx context.Context, db DBTX, envID, projectID uuid.UUID, name, hash string) (ApiKey, error) {
	var k ApiKey
	err := db.QueryRow(ctx,
		`INSERT INTO api_key (environment_id, project_id, name, hash)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, environment_id, project_id, name, scope, created_at, last_used_at`,
		envID, projectID, name, hash,
	).Scan(&k.ID, &k.EnvironmentID, &k.ProjectID, &k.Name, &k.Scope, &k.CreatedAt, &k.LastUsedAt)
	return k, err
}

func (r *Repo) ApiKeyByHash(ctx context.Context, db DBTX, hash string) (ApiKey, error) {
	var k ApiKey
	err := db.QueryRow(ctx,
		`SELECT id, environment_id, project_id, name, scope, created_at, last_used_at
		 FROM api_key WHERE hash = $1`, hash,
	).Scan(&k.ID, &k.EnvironmentID, &k.ProjectID, &k.Name, &k.Scope, &k.CreatedAt, &k.LastUsedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ApiKey{}, ErrNotFound
	}
	return k, err
}

func (r *Repo) ListApiKeys(ctx context.Context, db DBTX, envID uuid.UUID) ([]ApiKey, error) {
	rows, err := db.Query(ctx,
		`SELECT id, environment_id, project_id, name, scope, created_at, last_used_at
		 FROM api_key WHERE environment_id = $1 ORDER BY created_at`, envID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ApiKey
	for rows.Next() {
		var k ApiKey
		if err := rows.Scan(&k.ID, &k.EnvironmentID, &k.ProjectID, &k.Name, &k.Scope, &k.CreatedAt, &k.LastUsedAt); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// RevokeApiKey deletes the key identified by id, scoped to envID so a key
// from one environment can't be revoked via another's id. It returns
// ErrNotFound if no row matched (already revoked, wrong id, or wrong env).
func (r *Repo) RevokeApiKey(ctx context.Context, db DBTX, id, envID uuid.UUID) error {
	tag, err := db.Exec(ctx,
		`DELETE FROM api_key WHERE id = $1 AND environment_id = $2`, id, envID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// TouchApiKey updates last_used_at, but only when it's unset or more than 60
// seconds old. apiKeyGuard calls this on every delivery request, so without
// the guard this would be a write on the hot path for every single request;
// throttling it keeps last_used_at fresh enough for operators to tell a live
// key from a dead one without turning every read into a write.
func (r *Repo) TouchApiKey(ctx context.Context, db DBTX, id uuid.UUID) error {
	_, err := db.Exec(ctx,
		`UPDATE api_key SET last_used_at = now()
		 WHERE id = $1 AND (last_used_at IS NULL OR last_used_at < now() - interval '60 seconds')`,
		id)
	return err
}
