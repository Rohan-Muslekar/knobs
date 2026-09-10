package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/Rohan-Muslekar/knobs/internal/authz"
	"github.com/Rohan-Muslekar/knobs/internal/store"
)

// errForbidden means the caller is a member of the relevant organization but
// their role doesn't meet the minimum the request requires. Maps to HTTP 403.
//
// A caller who isn't a member at all is never told that — resolving,
// project, or environment resolves to store.ErrNotFound instead, so a
// non-member gets the same 404 as a genuinely missing row and can't probe
// for the existence of resources in organizations they don't belong to.
var errForbidden = errors.New("forbidden")

// authorizeOrg loads the caller's role in orgID and requires it to be at
// least min. Returns store.ErrNotFound if the caller isn't a member (hiding
// whether the org itself exists from callers who reuse it that way), or
// errForbidden if they're a member whose role falls short.
func (d Deps) authorizeOrg(ctx context.Context, db store.DBTX, userID, orgID uuid.UUID, min authz.Role) (authz.Role, error) {
	roleStr, err := d.Repo.MemberRole(ctx, db, orgID, userID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return "", store.ErrNotFound
		}
		return "", err
	}
	role := authz.Role(roleStr)
	if !role.AtLeast(min) {
		return role, errForbidden
	}
	return role, nil
}

// authorizeProject loads the project (store.ErrNotFound if it doesn't
// exist), then requires the caller to hold at least min in the project's
// organization. A non-member gets store.ErrNotFound too, so the project's
// existence isn't revealed to outsiders.
func (d Deps) authorizeProject(ctx context.Context, db store.DBTX, userID, projectID uuid.UUID, min authz.Role) (store.Project, authz.Role, error) {
	project, err := d.Repo.ProjectByID(ctx, db, projectID)
	if err != nil {
		return store.Project{}, "", err
	}
	role, err := d.authorizeOrg(ctx, db, userID, project.OrganizationID, min)
	if err != nil {
		return store.Project{}, "", err
	}
	return project, role, nil
}

// authorizeEnv loads the environment (store.ErrNotFound if it doesn't
// exist), then requires the caller to hold at least min in the
// organization that owns the environment's project. A non-member gets
// store.ErrNotFound too, so the environment's existence isn't revealed to
// outsiders.
func (d Deps) authorizeEnv(ctx context.Context, db store.DBTX, userID, envID uuid.UUID, min authz.Role) (store.Environment, authz.Role, error) {
	env, err := d.Repo.EnvironmentByID(ctx, db, envID)
	if err != nil {
		return store.Environment{}, "", err
	}
	orgID, err := d.Repo.OrgIDForEnv(ctx, db, envID)
	if err != nil {
		return store.Environment{}, "", err
	}
	role, err := d.authorizeOrg(ctx, db, userID, orgID, min)
	if err != nil {
		return store.Environment{}, "", err
	}
	return env, role, nil
}

// writeAuthzErr translates an error from authorizeOrg/authorizeProject/
// authorizeEnv into the right HTTP response: 404 for store.ErrNotFound
// (missing row or hidden non-member), 403 for errForbidden (a member whose
// role is too low), 500 for anything else. Returns true if it wrote a
// response, so handlers can write `if writeAuthzErr(w, err) { return }`.
func writeAuthzErr(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not found")
	case errors.Is(err, errForbidden):
		writeErr(w, http.StatusForbidden, "forbidden")
	default:
		writeErr(w, http.StatusInternalServerError, "internal error")
	}
	return true
}
