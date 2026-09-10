// Package authz defines the role hierarchy shared by organization
// membership: viewer < editor < admin < owner. It knows nothing about HTTP
// or storage — just the ranking, so both the API layer and any future
// caller (CLI, background jobs) can reuse the same rules.
package authz

// Role is an organization membership role, stored in organization_member.role
// as one of the four string values below.
type Role string

const (
	RoleViewer Role = "viewer"
	RoleEditor Role = "editor"
	RoleAdmin  Role = "admin"
	RoleOwner  Role = "owner"
)

// rank orders the roles from least to most privileged. A role missing from
// this map (the zero Role, or any unrecognized string) is not a valid role
// and ranks below every real one.
var rank = map[Role]int{
	RoleViewer: 0,
	RoleEditor: 1,
	RoleAdmin:  2,
	RoleOwner:  3,
}

// AtLeast reports whether r meets or exceeds min in the role hierarchy. An
// unrecognized role (on either side) is never at least anything — including
// itself — since it isn't a role the system understands.
func (r Role) AtLeast(min Role) bool {
	rr, ok := rank[r]
	if !ok {
		return false
	}
	mr, ok := rank[min]
	if !ok {
		return false
	}
	return rr >= mr
}

// Valid reports whether r is one of the four known roles.
func Valid(r Role) bool {
	_, ok := rank[r]
	return ok
}
