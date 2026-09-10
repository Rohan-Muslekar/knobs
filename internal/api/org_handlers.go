package api

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Rohan-Muslekar/knobs/internal/authz"
	"github.com/Rohan-Muslekar/knobs/internal/store"
)

// errLastOwner means the request would leave an organization with zero
// owners — demoting or removing its only owner. Maps to 409, distinct from
// the generic store errors WithTx callbacks otherwise return.
var errLastOwner = errors.New("cannot demote or remove the last owner")

type createOrganizationRequest struct {
	Name string `json:"name"`
}

// deriveSlug turns a display name into a URL-safe slug: lowercase,
// non-alphanumeric runs collapsed to a single hyphen, leading/trailing
// hyphens trimmed. An all-punctuation name falls back to "org" so callers
// never get an empty slug.
func deriveSlug(name string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	slug := strings.TrimRight(b.String(), "-")
	if slug == "" {
		slug = "org"
	}
	return slug
}

// generateTempPassword returns a random ~20-char URL-safe token, used as the
// one-time temporary password for a user created by handleAddMember. Mirrors
// internal/apikey.Generate's use of crypto/rand rather than reusing that
// package directly, since an org invite isn't an API key.
func generateTempPassword() (string, error) {
	buf := make([]byte, 15)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// handleCreateOrganization creates a new organization and makes the caller
// its owner, atomically. The slug is derived from the name; on a collision
// (another org already has that slug) it retries with a numeric suffix
// (-2, -3, ...) rather than failing outright, up to a bounded number of
// attempts.
func (d Deps) handleCreateOrganization(w http.ResponseWriter, r *http.Request) {
	var req createOrganizationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.Name == "" {
		writeErr(w, http.StatusUnprocessableEntity, "name required")
		return
	}
	// requireUser has already run on this route, so the id is always present;
	// the ok is discarded rather than checked again.
	uid, _ := currentUserID(r.Context())

	base := deriveSlug(req.Name)
	const maxSlugAttempts = 20
	var org store.Organization
	for attempt := 0; attempt < maxSlugAttempts; attempt++ {
		slug := base
		if attempt > 0 {
			slug = fmt.Sprintf("%s-%d", base, attempt+1)
		}
		err := d.Repo.WithTx(r.Context(), func(tx pgxTx) error {
			var e error
			org, e = d.Repo.CreateOrganization(r.Context(), tx, req.Name, slug)
			if e != nil {
				return e
			}
			if e := d.Repo.AddMember(r.Context(), tx, org.ID, uid, string(authz.RoleOwner)); e != nil {
				return e
			}
			return d.Repo.RecordAudit(r.Context(), tx, uuid.Nil, uid, "org.create", org.ID.String(),
				map[string]string{"name": org.Name, "slug": org.Slug})
		})
		if err == nil {
			writeJSON(w, http.StatusCreated, orgView(org))
			return
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			continue
		}
		writeErr(w, http.StatusInternalServerError, "could not create organization")
		return
	}
	writeErr(w, http.StatusConflict, "could not derive a unique slug for this name")
}

// handleListOrganizations has no authorize call: like handleListProjects,
// it isn't scoped to a single org — it returns every org the caller belongs
// to, each paired with the caller's own role in it.
func (d Deps) handleListOrganizations(w http.ResponseWriter, r *http.Request) {
	uid, _ := currentUserID(r.Context())
	orgs, err := d.Repo.ListOrganizationsForUser(r.Context(), d.Repo.Pool(), uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not list organizations")
		return
	}
	views := make([]map[string]any, 0, len(orgs))
	for _, o := range orgs {
		views = append(views, map[string]any{
			"id": o.ID, "name": o.Name, "slug": o.Slug, "role": o.Role, "createdAt": o.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, views)
}

func (d Deps) handleListMembers(w http.ResponseWriter, r *http.Request) {
	orgID, err := uuid.Parse(chi.URLParam(r, "orgID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid organization id")
		return
	}
	uid, _ := currentUserID(r.Context())
	if _, err := d.authorizeOrg(r.Context(), d.Repo.Pool(), uid, orgID, authz.RoleViewer); writeAuthzErr(w, err) {
		return
	}
	members, err := d.Repo.ListMembers(r.Context(), d.Repo.Pool(), orgID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not list members")
		return
	}
	views := make([]map[string]any, 0, len(members))
	for _, m := range members {
		views = append(views, map[string]any{
			"userId": m.UserID, "email": m.Email, "role": m.Role, "createdAt": m.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, views)
}

type addMemberRequest struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

// handleAddMember adds a member to an organization by email: an existing
// user is added directly, an unknown email gets a brand-new account with a
// random temporary password that is returned in the response exactly once
// and never logged or persisted in plaintext.
//
// The caller (an admin or owner, enforced by authorizeOrg) can never grant a
// role higher than their own — an admin can add viewers/editors/admins but
// not owners.
func (d Deps) handleAddMember(w http.ResponseWriter, r *http.Request) {
	orgID, err := uuid.Parse(chi.URLParam(r, "orgID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid organization id")
		return
	}
	var req addMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.Email == "" || !authz.Valid(authz.Role(req.Role)) {
		writeErr(w, http.StatusUnprocessableEntity, "email required and role must be one of viewer/editor/admin/owner")
		return
	}
	// requireUser has already run on this route, so the id is always present;
	// the ok is discarded rather than checked again.
	uid, _ := currentUserID(r.Context())
	callerRole, err := d.authorizeOrg(r.Context(), d.Repo.Pool(), uid, orgID, authz.RoleAdmin)
	if writeAuthzErr(w, err) {
		return
	}
	grantRole := authz.Role(req.Role)
	if !callerRole.AtLeast(grantRole) {
		writeErr(w, http.StatusForbidden, "cannot grant a role above your own")
		return
	}

	var (
		userID       uuid.UUID
		tempPassword string
		isNewUser    bool
	)
	err = d.Repo.WithTx(r.Context(), func(tx pgxTx) error {
		u, e := d.Repo.UserByEmail(r.Context(), tx, req.Email)
		switch {
		case e == nil:
			userID = u.ID
		case errors.Is(e, store.ErrNotFound):
			pw, genErr := generateTempPassword()
			if genErr != nil {
				return genErr
			}
			hash, hashErr := d.Auth.Hash(pw)
			if hashErr != nil {
				return hashErr
			}
			newUser, createErr := d.Repo.CreateUser(r.Context(), tx, req.Email, hash)
			if createErr != nil {
				return createErr
			}
			userID = newUser.ID
			tempPassword = pw
			isNewUser = true
		default:
			return e
		}
		if e := d.Repo.AddMember(r.Context(), tx, orgID, userID, req.Role); e != nil {
			return e
		}
		return d.Repo.RecordAudit(r.Context(), tx, uuid.Nil, uid, "org.member.add", userID.String(),
			map[string]string{"email": req.Email, "role": req.Role})
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeErr(w, http.StatusConflict, "already a member")
			return
		}
		writeErr(w, http.StatusInternalServerError, "could not add member")
		return
	}
	resp := map[string]any{"userId": userID, "email": req.Email, "role": req.Role}
	if isNewUser {
		// Shown exactly once: this response is the only place the plaintext
		// temporary password ever appears. It is never logged.
		resp["temporaryPassword"] = tempPassword
	}
	writeJSON(w, http.StatusCreated, resp)
}

type updateMemberRoleRequest struct {
	Role string `json:"role"`
}

// handleUpdateMemberRole changes a member's role. Only an owner may call
// this. Demoting an organization's last owner to a non-owner role is
// rejected with 409 — someone has to stay in charge.
func (d Deps) handleUpdateMemberRole(w http.ResponseWriter, r *http.Request) {
	orgID, err := uuid.Parse(chi.URLParam(r, "orgID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid organization id")
		return
	}
	targetID, err := uuid.Parse(chi.URLParam(r, "userID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid user id")
		return
	}
	var req updateMemberRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if !authz.Valid(authz.Role(req.Role)) {
		writeErr(w, http.StatusUnprocessableEntity, "role must be one of viewer/editor/admin/owner")
		return
	}
	// requireUser has already run on this route, so the id is always present;
	// the ok is discarded rather than checked again.
	uid, _ := currentUserID(r.Context())
	if _, err := d.authorizeOrg(r.Context(), d.Repo.Pool(), uid, orgID, authz.RoleOwner); writeAuthzErr(w, err) {
		return
	}
	err = d.Repo.WithTx(r.Context(), func(tx pgxTx) error {
		currentRole, e := d.Repo.MemberRole(r.Context(), tx, orgID, targetID)
		if e != nil {
			return e
		}
		if currentRole == string(authz.RoleOwner) && req.Role != string(authz.RoleOwner) {
			// Lock the owner rows before counting: two concurrent
			// demotes on the same org would otherwise both read the
			// pre-demote count under READ COMMITTED and both pass.
			n, cntErr := d.Repo.CountOwnersForUpdate(r.Context(), tx, orgID)
			if cntErr != nil {
				return cntErr
			}
			if n <= 1 {
				return errLastOwner
			}
		}
		if e := d.Repo.UpdateMemberRole(r.Context(), tx, orgID, targetID, req.Role); e != nil {
			return e
		}
		return d.Repo.RecordAudit(r.Context(), tx, uuid.Nil, uid, "org.member.role_change", targetID.String(),
			map[string]string{"role": req.Role})
	})
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, map[string]any{"userId": targetID, "role": req.Role})
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, http.StatusNotFound, "member not found")
	case errors.Is(err, errLastOwner):
		writeErr(w, http.StatusConflict, "cannot demote the last owner")
	default:
		writeErr(w, http.StatusInternalServerError, "could not update member role")
	}
}

// handleRemoveMember removes a member from an organization. Only an owner
// may call this. Removing an organization's last owner is rejected with 409.
func (d Deps) handleRemoveMember(w http.ResponseWriter, r *http.Request) {
	orgID, err := uuid.Parse(chi.URLParam(r, "orgID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid organization id")
		return
	}
	targetID, err := uuid.Parse(chi.URLParam(r, "userID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid user id")
		return
	}
	// requireUser has already run on this route, so the id is always present;
	// the ok is discarded rather than checked again.
	uid, _ := currentUserID(r.Context())
	if _, err := d.authorizeOrg(r.Context(), d.Repo.Pool(), uid, orgID, authz.RoleOwner); writeAuthzErr(w, err) {
		return
	}
	err = d.Repo.WithTx(r.Context(), func(tx pgxTx) error {
		currentRole, e := d.Repo.MemberRole(r.Context(), tx, orgID, targetID)
		if e != nil {
			return e
		}
		if currentRole == string(authz.RoleOwner) {
			// Lock the owner rows before counting: two concurrent
			// removes on the same org would otherwise both read the
			// pre-remove count under READ COMMITTED and both pass.
			n, cntErr := d.Repo.CountOwnersForUpdate(r.Context(), tx, orgID)
			if cntErr != nil {
				return cntErr
			}
			if n <= 1 {
				return errLastOwner
			}
		}
		if e := d.Repo.RemoveMember(r.Context(), tx, orgID, targetID); e != nil {
			return e
		}
		return d.Repo.RecordAudit(r.Context(), tx, uuid.Nil, uid, "org.member.remove", targetID.String(), nil)
	})
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, http.StatusNotFound, "member not found")
	case errors.Is(err, errLastOwner):
		writeErr(w, http.StatusConflict, "cannot remove the last owner")
	default:
		writeErr(w, http.StatusInternalServerError, "could not remove member")
	}
}

func orgView(o store.Organization) map[string]any {
	return map[string]any{"id": o.ID, "name": o.Name, "slug": o.Slug, "createdAt": o.CreatedAt}
}
