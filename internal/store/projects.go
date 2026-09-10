package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Project struct {
	ID             uuid.UUID
	OrganizationID uuid.UUID
	Name           string
	Slug           string
	CreatedAt      time.Time
}

func (r *Repo) CreateProject(ctx context.Context, db DBTX, organizationID uuid.UUID, name, slug string) (Project, error) {
	var p Project
	err := db.QueryRow(ctx,
		`INSERT INTO project (organization_id, name, slug) VALUES ($1, $2, $3)
		 RETURNING id, organization_id, name, slug, created_at`, organizationID, name, slug,
	).Scan(&p.ID, &p.OrganizationID, &p.Name, &p.Slug, &p.CreatedAt)
	return p, err
}

func (r *Repo) ListProjects(ctx context.Context, db DBTX) ([]Project, error) {
	rows, err := db.Query(ctx, `SELECT id, organization_id, name, slug, created_at FROM project ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Project
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.ID, &p.OrganizationID, &p.Name, &p.Slug, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ListProjectsForUser returns every project belonging to an organization the
// given user is a member of, across all such organizations.
func (r *Repo) ListProjectsForUser(ctx context.Context, db DBTX, userID uuid.UUID) ([]Project, error) {
	rows, err := db.Query(ctx,
		`SELECT p.id, p.organization_id, p.name, p.slug, p.created_at
		 FROM project p
		 JOIN organization_member m ON m.organization_id = p.organization_id
		 WHERE m.user_id = $1
		 ORDER BY p.created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Project
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.ID, &p.OrganizationID, &p.Name, &p.Slug, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *Repo) ProjectByID(ctx context.Context, db DBTX, id uuid.UUID) (Project, error) {
	var p Project
	err := db.QueryRow(ctx,
		`SELECT id, organization_id, name, slug, created_at FROM project WHERE id = $1`, id,
	).Scan(&p.ID, &p.OrganizationID, &p.Name, &p.Slug, &p.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	return p, err
}

func (r *Repo) UpdateProjectName(ctx context.Context, db DBTX, id uuid.UUID, name string) (Project, error) {
	var p Project
	err := db.QueryRow(ctx,
		`UPDATE project SET name = $2 WHERE id = $1
		 RETURNING id, organization_id, name, slug, created_at`, id, name,
	).Scan(&p.ID, &p.OrganizationID, &p.Name, &p.Slug, &p.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	return p, err
}
