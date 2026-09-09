package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Environment struct {
	ID               uuid.UUID
	ProjectID        uuid.UUID
	Name             string
	CurrentVersionID *uuid.UUID
	CreatedAt        time.Time
	// DeliveryRevision is the monotonic per-environment counter used as the
	// dedup/since axis for delivery (snapshot/stream), separate from the
	// user-facing config Version: a rollback can move Version backwards, but
	// DeliveryRevision only ever increases. Only populated by EnvironmentByID
	// today, since that's the only lookup delivery routes use.
	DeliveryRevision int64
}

func (r *Repo) CreateEnvironment(ctx context.Context, db DBTX, projectID uuid.UUID, name string) (Environment, error) {
	var e Environment
	err := db.QueryRow(ctx,
		`INSERT INTO environment (project_id, name) VALUES ($1, $2)
		 RETURNING id, project_id, name, current_version_id, created_at`, projectID, name,
	).Scan(&e.ID, &e.ProjectID, &e.Name, &e.CurrentVersionID, &e.CreatedAt)
	return e, err
}

func (r *Repo) ListEnvironments(ctx context.Context, db DBTX, projectID uuid.UUID) ([]Environment, error) {
	rows, err := db.Query(ctx,
		`SELECT id, project_id, name, current_version_id, created_at
		 FROM environment WHERE project_id = $1 ORDER BY created_at`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Environment
	for rows.Next() {
		var e Environment
		if err := rows.Scan(&e.ID, &e.ProjectID, &e.Name, &e.CurrentVersionID, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (r *Repo) EnvironmentByID(ctx context.Context, db DBTX, id uuid.UUID) (Environment, error) {
	var e Environment
	err := db.QueryRow(ctx,
		`SELECT id, project_id, name, current_version_id, created_at, delivery_revision
		 FROM environment WHERE id = $1`, id,
	).Scan(&e.ID, &e.ProjectID, &e.Name, &e.CurrentVersionID, &e.CreatedAt, &e.DeliveryRevision)
	if errors.Is(err, pgx.ErrNoRows) {
		return Environment{}, ErrNotFound
	}
	return e, err
}
