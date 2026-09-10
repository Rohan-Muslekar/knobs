//go:build integration

package api_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/Rohan-Muslekar/knobs/internal/auth"
	"github.com/Rohan-Muslekar/knobs/internal/store"
)

// TestOrganizationsAndMembers drives organization creation and the full
// membership lifecycle through the real HTTP surface: create org -> caller
// is owner and it shows up in /auth/me, list members, add an existing user,
// add a brand-new email (temp password shown once, usable to log in),
// the role-grant ceiling, the already-a-member conflict, the admin-only
// gate on adding members, the owner-only gate on role changes, and the
// last-owner guards on both demotion and removal.
func TestOrganizationsAndMembers(t *testing.T) {
	router, ownerCookie, repo, _ := seededRouter(t)
	ctx := t.Context()

	// Mints session tokens directly for users seeded via the repo rather
	// than through the login endpoint. Verification is purely a function of
	// the JWT secret, so a second Authenticator built from the same secret
	// seededRouter used ("test-secret", see projects_handlers_test.go)
	// issues tokens the router's own Authenticator will accept.
	authr := auth.New("test-secret", false)
	issue := func(id uuid.UUID) string {
		t.Helper()
		tok, err := authr.Issue(id)
		if err != nil {
			t.Fatalf("issue token: %v", err)
		}
		return tok
	}
	mkUser := func(email string) store.User {
		t.Helper()
		hash, _ := authr.Hash("pw")
		u, err := repo.CreateUser(ctx, repo.Pool(), email, hash)
		if err != nil {
			t.Fatalf("seed user %s: %v", email, err)
		}
		return u
	}

	// --- create organization: caller becomes owner, org shows up in
	// /auth/me with that role. ---
	createRec := post(router, "/v1/organizations", `{"name":"Widget Co"}`, ownerCookie)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create org = %d, want 201, body=%s", createRec.Code, createRec.Body.String())
	}
	var org map[string]any
	if err := json.NewDecoder(createRec.Body).Decode(&org); err != nil {
		t.Fatalf("decode created org: %v", err)
	}
	orgID, _ := org["id"].(string)
	if orgID == "" {
		t.Fatal("create org returned no id")
	}
	if org["slug"] != "widget-co" {
		t.Fatalf("slug = %v, want widget-co", org["slug"])
	}

	meRec := get(router, "/v1/auth/me", ownerCookie)
	if meRec.Code != http.StatusOK {
		t.Fatalf("me = %d, want 200, body=%s", meRec.Code, meRec.Body.String())
	}
	var me map[string]any
	if err := json.NewDecoder(meRec.Body).Decode(&me); err != nil {
		t.Fatalf("decode me: %v", err)
	}
	orgs, _ := me["organizations"].([]any)
	foundOrg := false
	for _, o := range orgs {
		om, _ := o.(map[string]any)
		if om["id"] == orgID {
			foundOrg = true
			if om["role"] != "owner" {
				t.Fatalf("caller role in new org = %v, want owner", om["role"])
			}
		}
	}
	if !foundOrg {
		t.Fatalf("new org %s not in /auth/me organizations: %v", orgID, orgs)
	}

	// --- seed an admin and a viewer directly, plus an existing user (not
	// yet a member) and an outsider (never added to this org at all). ---
	oid := uuid.MustParse(orgID)
	adminUser := mkUser("admin@x.com")
	viewerUser := mkUser("viewer@x.com")
	existing := mkUser("existing@x.com")
	outsider := mkUser("outsider@x.com")
	if err := repo.AddMember(ctx, repo.Pool(), oid, adminUser.ID, "admin"); err != nil {
		t.Fatalf("seed admin membership: %v", err)
	}
	if err := repo.AddMember(ctx, repo.Pool(), oid, viewerUser.ID, "viewer"); err != nil {
		t.Fatalf("seed viewer membership: %v", err)
	}
	adminCookie := issue(adminUser.ID)
	viewerCookie := issue(viewerUser.ID)
	outsiderCookie := issue(outsider.ID)

	membersPath := "/v1/organizations/" + orgID + "/members"

	// --- list members: owner + admin + viewer seeded so far. ---
	listRec := get(router, membersPath, ownerCookie)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list members = %d, want 200, body=%s", listRec.Code, listRec.Body.String())
	}
	var members []map[string]any
	if err := json.NewDecoder(listRec.Body).Decode(&members); err != nil {
		t.Fatalf("decode members: %v", err)
	}
	if len(members) != 3 {
		t.Fatalf("members = %d, want 3: %v", len(members), members)
	}

	// --- add an EXISTING user as editor: no temporaryPassword in the
	// response, since no account was created. ---
	addRec := post(router, membersPath, `{"email":"existing@x.com","role":"editor"}`, ownerCookie)
	if addRec.Code != http.StatusCreated {
		t.Fatalf("add existing member = %d, want 201, body=%s", addRec.Code, addRec.Body.String())
	}
	var addBody map[string]any
	_ = json.NewDecoder(addRec.Body).Decode(&addBody)
	if _, ok := addBody["temporaryPassword"]; ok {
		t.Fatalf("adding an existing user must not return a temporaryPassword: %v", addBody)
	}
	if addBody["userId"] != existing.ID.String() {
		t.Fatalf("add existing member userId = %v, want %v", addBody["userId"], existing.ID)
	}

	// --- add a NEW email: the response carries a temp password exactly
	// once, and the new user can log in with it. ---
	newRec := post(router, membersPath, `{"email":"brandnew@x.com","role":"viewer"}`, ownerCookie)
	if newRec.Code != http.StatusCreated {
		t.Fatalf("add new member = %d, want 201, body=%s", newRec.Code, newRec.Body.String())
	}
	var newBody map[string]any
	_ = json.NewDecoder(newRec.Body).Decode(&newBody)
	tempPassword, _ := newBody["temporaryPassword"].(string)
	if tempPassword == "" {
		t.Fatalf("add new member returned no temporaryPassword: %v", newBody)
	}
	loginRec := post(router, "/v1/auth/login", `{"email":"brandnew@x.com","password":"`+tempPassword+`"}`, "")
	if loginRec.Code != http.StatusOK {
		t.Fatalf("new member login with temp password = %d, want 200, body=%s", loginRec.Code, loginRec.Body.String())
	}

	// --- role-grant ceiling: an admin cannot mint an owner. ---
	ceilRec := post(router, membersPath, `{"email":"nobody-yet@x.com","role":"owner"}`, adminCookie)
	if ceilRec.Code != http.StatusForbidden {
		t.Fatalf("admin granting owner = %d, want 403, body=%s", ceilRec.Code, ceilRec.Body.String())
	}

	// --- already a member -> 409. ---
	dupRec := post(router, membersPath, `{"email":"existing@x.com","role":"viewer"}`, ownerCookie)
	if dupRec.Code != http.StatusConflict {
		t.Fatalf("re-add existing member = %d, want 409, body=%s", dupRec.Code, dupRec.Body.String())
	}

	// --- adding a member requires admin: a viewer gets 403 ... ---
	viewerAddRec := post(router, membersPath, `{"email":"someone@x.com","role":"viewer"}`, viewerCookie)
	if viewerAddRec.Code != http.StatusForbidden {
		t.Fatalf("viewer adding a member = %d, want 403, body=%s", viewerAddRec.Code, viewerAddRec.Body.String())
	}
	// ... and an outsider (not a member at all) gets 404, existence hidden.
	outsiderAddRec := post(router, membersPath, `{"email":"someone@x.com","role":"viewer"}`, outsiderCookie)
	if outsiderAddRec.Code != http.StatusNotFound {
		t.Fatalf("outsider adding a member = %d, want 404, body=%s", outsiderAddRec.Code, outsiderAddRec.Body.String())
	}

	// --- changing a role requires owner: an admin gets 403. ---
	roleChangeRec := patch(router, membersPath+"/"+existing.ID.String(), `{"role":"admin"}`, adminCookie)
	if roleChangeRec.Code != http.StatusForbidden {
		t.Fatalf("admin changing a role = %d, want 403, body=%s", roleChangeRec.Code, roleChangeRec.Body.String())
	}

	ownerUser, err := repo.UserByEmail(ctx, repo.Pool(), "a@x.com")
	if err != nil {
		t.Fatalf("lookup owner user: %v", err)
	}

	// --- last-owner demote -> 409. ---
	demoteRec := patch(router, membersPath+"/"+ownerUser.ID.String(), `{"role":"editor"}`, ownerCookie)
	if demoteRec.Code != http.StatusConflict {
		t.Fatalf("demote last owner = %d, want 409, body=%s", demoteRec.Code, demoteRec.Body.String())
	}

	// --- the owner changing a non-owner's role succeeds. ---
	roleOKRec := patch(router, membersPath+"/"+existing.ID.String(), `{"role":"admin"}`, ownerCookie)
	if roleOKRec.Code != http.StatusOK {
		t.Fatalf("owner changing existing's role = %d, want 200, body=%s", roleOKRec.Code, roleOKRec.Body.String())
	}

	// --- last-owner remove -> 409. ---
	removeOwnerRec := del(router, membersPath+"/"+ownerUser.ID.String(), ownerCookie)
	if removeOwnerRec.Code != http.StatusConflict {
		t.Fatalf("remove last owner = %d, want 409, body=%s", removeOwnerRec.Code, removeOwnerRec.Body.String())
	}

	// --- removing a non-last member succeeds. ---
	removeRec := del(router, membersPath+"/"+viewerUser.ID.String(), ownerCookie)
	if removeRec.Code != http.StatusNoContent {
		t.Fatalf("remove non-last member = %d, want 204, body=%s", removeRec.Code, removeRec.Body.String())
	}
}
