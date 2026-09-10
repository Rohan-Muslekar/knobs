package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Organization struct {
	ID        uuid.UUID
	Name      string
	Slug      string
	CreatedAt time.Time
}

// OrgWithRole is an Organization alongside the role a specific user holds in
// it — the shape ListOrganizationsForUser returns, since "which orgs can I
// see" is always asked together with "what can I do in each one".
type OrgWithRole struct {
	Organization
	Role string
}

// Member is one row of an organization's membership list, joined out to the
// member's email since callers listing members want to show who they are,
// not just their id.
type Member struct {
	UserID    uuid.UUID
	Email     string
	Role      string
	CreatedAt time.Time
}

func (r *Repo) CreateOrganization(ctx context.Context, db DBTX, name, slug string) (Organization, error) {
	var o Organization
	err := db.QueryRow(ctx,
		`INSERT INTO organization (name, slug) VALUES ($1, $2)
		 RETURNING id, name, slug, created_at`, name, slug,
	).Scan(&o.ID, &o.Name, &o.Slug, &o.CreatedAt)
	return o, err
}

func (r *Repo) OrganizationByID(ctx context.Context, db DBTX, id uuid.UUID) (Organization, error) {
	var o Organization
	err := db.QueryRow(ctx,
		`SELECT id, name, slug, created_at FROM organization WHERE id = $1`, id,
	).Scan(&o.ID, &o.Name, &o.Slug, &o.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Organization{}, ErrNotFound
	}
	return o, err
}

// OrganizationBySlug looks up an organization by its unique slug — used by
// the admin seed to find the "default" org the migration creates.
func (r *Repo) OrganizationBySlug(ctx context.Context, db DBTX, slug string) (Organization, error) {
	var o Organization
	err := db.QueryRow(ctx,
		`SELECT id, name, slug, created_at FROM organization WHERE slug = $1`, slug,
	).Scan(&o.ID, &o.Name, &o.Slug, &o.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Organization{}, ErrNotFound
	}
	return o, err
}

// ListOrganizationsForUser returns every organization the given user is a
// member of, along with the role they hold in each.
func (r *Repo) ListOrganizationsForUser(ctx context.Context, db DBTX, userID uuid.UUID) ([]OrgWithRole, error) {
	rows, err := db.Query(ctx,
		`SELECT o.id, o.name, o.slug, o.created_at, m.role
		 FROM organization o
		 JOIN organization_member m ON m.organization_id = o.id
		 WHERE m.user_id = $1
		 ORDER BY o.name, o.created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OrgWithRole
	for rows.Next() {
		var ow OrgWithRole
		if err := rows.Scan(&ow.ID, &ow.Name, &ow.Slug, &ow.CreatedAt, &ow.Role); err != nil {
			return nil, err
		}
		out = append(out, ow)
	}
	return out, rows.Err()
}

// MemberRole returns the role a user holds in an organization, or
// ErrNotFound if they are not a member.
func (r *Repo) MemberRole(ctx context.Context, db DBTX, orgID, userID uuid.UUID) (string, error) {
	var role string
	err := db.QueryRow(ctx,
		`SELECT role FROM organization_member WHERE organization_id = $1 AND user_id = $2`,
		orgID, userID,
	).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return role, err
}

// AddMember inserts a membership row. On a duplicate (org, user) pair this
// returns the underlying pg error (code 23505) unwrapped, so callers such as
// the API layer can map it to a 409 themselves rather than losing the
// pgconn.PgError behind a generic wrapper.
func (r *Repo) AddMember(ctx context.Context, db DBTX, orgID, userID uuid.UUID, role string) error {
	_, err := db.Exec(ctx,
		`INSERT INTO organization_member (organization_id, user_id, role) VALUES ($1, $2, $3)`,
		orgID, userID, role)
	return err
}

// UpdateMemberRole changes an existing member's role. ErrNotFound if there
// is no such membership row.
func (r *Repo) UpdateMemberRole(ctx context.Context, db DBTX, orgID, userID uuid.UUID, role string) error {
	tag, err := db.Exec(ctx,
		`UPDATE organization_member SET role = $3 WHERE organization_id = $1 AND user_id = $2`,
		orgID, userID, role)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// RemoveMember deletes a membership row. ErrNotFound if there is no such row.
func (r *Repo) RemoveMember(ctx context.Context, db DBTX, orgID, userID uuid.UUID) error {
	tag, err := db.Exec(ctx,
		`DELETE FROM organization_member WHERE organization_id = $1 AND user_id = $2`,
		orgID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListMembers returns every member of an organization, joined out to their
// email, ordered by email.
func (r *Repo) ListMembers(ctx context.Context, db DBTX, orgID uuid.UUID) ([]Member, error) {
	rows, err := db.Query(ctx,
		`SELECT m.user_id, u.email, m.role, m.created_at
		 FROM organization_member m
		 JOIN app_user u ON u.id = m.user_id
		 WHERE m.organization_id = $1
		 ORDER BY u.email`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Member
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.UserID, &m.Email, &m.Role, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// OrgIDForProject resolves the organization a project belongs to.
// ErrNotFound if the project does not exist.
func (r *Repo) OrgIDForProject(ctx context.Context, db DBTX, projectID uuid.UUID) (uuid.UUID, error) {
	var orgID uuid.UUID
	err := db.QueryRow(ctx,
		`SELECT organization_id FROM project WHERE id = $1`, projectID,
	).Scan(&orgID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrNotFound
	}
	return orgID, err
}

// OrgIDForEnv resolves the organization an environment's project belongs to,
// by joining through project. ErrNotFound if the environment does not exist.
func (r *Repo) OrgIDForEnv(ctx context.Context, db DBTX, envID uuid.UUID) (uuid.UUID, error) {
	var orgID uuid.UUID
	err := db.QueryRow(ctx,
		`SELECT p.organization_id
		 FROM environment e
		 JOIN project p ON p.id = e.project_id
		 WHERE e.id = $1`, envID,
	).Scan(&orgID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrNotFound
	}
	return orgID, err
}

// CountOwners counts how many members of an organization hold the "owner"
// role — used by the last-owner guard when removing/demoting a member.
func (r *Repo) CountOwners(ctx context.Context, db DBTX, orgID uuid.UUID) (int, error) {
	var n int
	err := db.QueryRow(ctx,
		`SELECT count(*) FROM organization_member WHERE organization_id = $1 AND role = 'owner'`,
		orgID,
	).Scan(&n)
	return n, err
}
