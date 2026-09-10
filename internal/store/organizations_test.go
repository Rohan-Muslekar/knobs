//go:build integration

package store_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Rohan-Muslekar/knobs/internal/store"
)

// TestDefaultOrgAndBackfill covers the 00007_rbac migration itself: the
// "default" organization it seeds must exist, and a project created after
// the migration must land with a non-null organization_id.
func TestDefaultOrgAndBackfill(t *testing.T) {
	ctx := context.Background()
	repo := store.New(migratedPool(t))

	def, err := repo.OrganizationBySlug(ctx, repo.Pool(), "default")
	if err != nil {
		t.Fatalf("default org: %v", err)
	}
	if def.Name != "Default" {
		t.Fatalf("default org name = %q, want %q", def.Name, "Default")
	}

	p, err := repo.CreateProject(ctx, repo.Pool(), def.ID, "Acme", "acme")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if p.OrganizationID != def.ID {
		t.Fatalf("project.OrganizationID = %v, want %v", p.OrganizationID, def.ID)
	}

	got, err := repo.ProjectByID(ctx, repo.Pool(), p.ID)
	if err != nil {
		t.Fatalf("project by id: %v", err)
	}
	if got.OrganizationID != def.ID {
		t.Fatalf("ProjectByID.OrganizationID = %v, want %v", got.OrganizationID, def.ID)
	}
}

// TestOrganizationsAndMembership covers CreateOrganization, AddMember,
// ListOrganizationsForUser and MemberRole (including the ErrNotFound case
// for a non-member).
func TestOrganizationsAndMembership(t *testing.T) {
	ctx := context.Background()
	repo := store.New(migratedPool(t))

	org, err := repo.CreateOrganization(ctx, repo.Pool(), "Acme Corp", "acme-corp")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}

	user, err := repo.CreateUser(ctx, repo.Pool(), "member@acme.test", "hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	other, err := repo.CreateUser(ctx, repo.Pool(), "outsider@acme.test", "hash")
	if err != nil {
		t.Fatalf("create other user: %v", err)
	}

	if err := repo.AddMember(ctx, repo.Pool(), org.ID, user.ID, "editor"); err != nil {
		t.Fatalf("add member: %v", err)
	}

	role, err := repo.MemberRole(ctx, repo.Pool(), org.ID, user.ID)
	if err != nil {
		t.Fatalf("member role: %v", err)
	}
	if role != "editor" {
		t.Fatalf("role = %q, want %q", role, "editor")
	}

	if _, err := repo.MemberRole(ctx, repo.Pool(), org.ID, other.ID); err != store.ErrNotFound {
		t.Fatalf("member role (non-member) err = %v, want store.ErrNotFound", err)
	}

	orgs, err := repo.ListOrganizationsForUser(ctx, repo.Pool(), user.ID)
	if err != nil {
		t.Fatalf("list orgs for user: %v", err)
	}
	if len(orgs) != 1 {
		t.Fatalf("list orgs for user len = %d, want 1", len(orgs))
	}
	if orgs[0].ID != org.ID || orgs[0].Role != "editor" {
		t.Fatalf("orgs[0] = %+v, want org %v with role editor", orgs[0], org.ID)
	}

	// Not a member of anything: an empty (non-error) result.
	empty, err := repo.ListOrganizationsForUser(ctx, repo.Pool(), other.ID)
	if err != nil {
		t.Fatalf("list orgs for outsider: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("list orgs for outsider len = %d, want 0", len(empty))
	}

	// ListMembers returns the member joined out to their email.
	members, err := repo.ListMembers(ctx, repo.Pool(), org.ID)
	if err != nil {
		t.Fatalf("list members: %v", err)
	}
	if len(members) != 1 || members[0].Email != "member@acme.test" || members[0].Role != "editor" {
		t.Fatalf("members = %+v, want one editor member@acme.test", members)
	}

	// UpdateMemberRole happy path + ErrNotFound for a non-member.
	if err := repo.UpdateMemberRole(ctx, repo.Pool(), org.ID, user.ID, "admin"); err != nil {
		t.Fatalf("update member role: %v", err)
	}
	role, err = repo.MemberRole(ctx, repo.Pool(), org.ID, user.ID)
	if err != nil || role != "admin" {
		t.Fatalf("role after update = %q, err=%v, want admin", role, err)
	}
	if err := repo.UpdateMemberRole(ctx, repo.Pool(), org.ID, other.ID, "admin"); err != store.ErrNotFound {
		t.Fatalf("update role (non-member) err = %v, want store.ErrNotFound", err)
	}

	// RemoveMember happy path + ErrNotFound on a second removal.
	if err := repo.RemoveMember(ctx, repo.Pool(), org.ID, user.ID); err != nil {
		t.Fatalf("remove member: %v", err)
	}
	if _, err := repo.MemberRole(ctx, repo.Pool(), org.ID, user.ID); err != store.ErrNotFound {
		t.Fatalf("member role after removal err = %v, want store.ErrNotFound", err)
	}
	if err := repo.RemoveMember(ctx, repo.Pool(), org.ID, user.ID); err != store.ErrNotFound {
		t.Fatalf("remove member (already gone) err = %v, want store.ErrNotFound", err)
	}
}

// TestAddMemberDuplicateConflict verifies that adding the same (org, user)
// membership twice surfaces the underlying pg unique-violation rather than
// silently succeeding, so the API layer (task 4) can map it to a 409.
func TestAddMemberDuplicateConflict(t *testing.T) {
	ctx := context.Background()
	repo := store.New(migratedPool(t))

	org, err := repo.CreateOrganization(ctx, repo.Pool(), "Acme Corp", "acme-corp")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	user, err := repo.CreateUser(ctx, repo.Pool(), "member@acme.test", "hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.AddMember(ctx, repo.Pool(), org.ID, user.ID, "viewer"); err != nil {
		t.Fatalf("add member: %v", err)
	}
	if err := repo.AddMember(ctx, repo.Pool(), org.ID, user.ID, "viewer"); err == nil {
		t.Fatal("expected duplicate membership rejection")
	}
}

// TestCountOwners covers the last-owner-guard helper.
func TestCountOwners(t *testing.T) {
	ctx := context.Background()
	repo := store.New(migratedPool(t))

	org, err := repo.CreateOrganization(ctx, repo.Pool(), "Acme Corp", "acme-corp")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	owner, err := repo.CreateUser(ctx, repo.Pool(), "owner@acme.test", "hash")
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	editor, err := repo.CreateUser(ctx, repo.Pool(), "editor@acme.test", "hash")
	if err != nil {
		t.Fatalf("create editor: %v", err)
	}

	if n, err := repo.CountOwners(ctx, repo.Pool(), org.ID); err != nil || n != 0 {
		t.Fatalf("count owners (none yet) = %d, err=%v, want 0", n, err)
	}

	if err := repo.AddMember(ctx, repo.Pool(), org.ID, owner.ID, "owner"); err != nil {
		t.Fatalf("add owner: %v", err)
	}
	if err := repo.AddMember(ctx, repo.Pool(), org.ID, editor.ID, "editor"); err != nil {
		t.Fatalf("add editor: %v", err)
	}

	if n, err := repo.CountOwners(ctx, repo.Pool(), org.ID); err != nil || n != 1 {
		t.Fatalf("count owners = %d, err=%v, want 1", n, err)
	}
}

// errLastOwnerForTest mirrors internal/api's errLastOwner sentinel — this
// test lives in the store package and reimplements the same guard shape
// (lock owner rows, count, refuse to drop below one) that the handlers use,
// so it needs its own sentinel rather than importing internal/api.
var errLastOwnerForTest = errors.New("cannot demote or remove the last owner")

// TestCountOwnersForUpdateSerializesLastOwnerGuard proves the fix for the
// TOCTOU race in the last-owner guard: with an org that has two owners, two
// concurrent transactions each try to demote one owner to viewer, replicating
// exactly what handleUpdateMemberRole does (CountOwnersForUpdate, then only
// demote if the locked count is still >1).
//
// Before the fix (plain CountOwners, no row lock) both transactions could
// read count==2 under READ COMMITTED and both proceed, leaving the org with
// zero owners. With CountOwnersForUpdate locking the owner rows, the second
// transaction blocks until the first commits, then re-reads the true
// post-commit count (1) and refuses. Run with -race and a high -count to
// make sure this holds up under repetition, not just once by luck.
func TestCountOwnersForUpdateSerializesLastOwnerGuard(t *testing.T) {
	ctx := context.Background()
	repo := store.New(migratedPool(t))

	org, err := repo.CreateOrganization(ctx, repo.Pool(), "Acme Corp", "acme-corp")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	ownerA, err := repo.CreateUser(ctx, repo.Pool(), "ownera@acme.test", "hash")
	if err != nil {
		t.Fatalf("create ownerA: %v", err)
	}
	ownerB, err := repo.CreateUser(ctx, repo.Pool(), "ownerb@acme.test", "hash")
	if err != nil {
		t.Fatalf("create ownerB: %v", err)
	}
	if err := repo.AddMember(ctx, repo.Pool(), org.ID, ownerA.ID, "owner"); err != nil {
		t.Fatalf("add ownerA: %v", err)
	}
	if err := repo.AddMember(ctx, repo.Pool(), org.ID, ownerB.ID, "owner"); err != nil {
		t.Fatalf("add ownerB: %v", err)
	}

	// demote replays the handler's guard: lock the owner rows, count them,
	// and only demote the target if that locked count is still above 1.
	demote := func(target uuid.UUID) error {
		return repo.WithTx(ctx, func(tx pgx.Tx) error {
			n, err := repo.CountOwnersForUpdate(ctx, tx, org.ID)
			if err != nil {
				return err
			}
			if n <= 1 {
				return errLastOwnerForTest
			}
			return repo.UpdateMemberRole(ctx, tx, org.ID, target, "viewer")
		})
	}

	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(2)
	go func() { defer wg.Done(); errs[0] = demote(ownerA.ID) }()
	go func() { defer wg.Done(); errs[1] = demote(ownerB.ID) }()
	wg.Wait()

	succeeded := 0
	for i, e := range errs {
		switch {
		case e == nil:
			succeeded++
		case errors.Is(e, errLastOwnerForTest):
			// expected outcome for the loser of the row lock
		default:
			t.Fatalf("demote %d: unexpected error: %v", i, e)
		}
	}
	if succeeded != 1 {
		t.Fatalf("expected exactly one demote to succeed and one to be rejected, got %d successes", succeeded)
	}

	n, err := repo.CountOwners(ctx, repo.Pool(), org.ID)
	if err != nil {
		t.Fatalf("count owners after concurrent demotes: %v", err)
	}
	if n < 1 {
		t.Fatalf("owner count after concurrent demotes = %d, want >= 1 (invariant violated)", n)
	}
}

// TestOrgIDForProjectAndEnv covers OrgIDForProject and OrgIDForEnv, the two
// lookups authz middleware (task 3) will use to resolve a request's tenant.
func TestOrgIDForProjectAndEnv(t *testing.T) {
	ctx := context.Background()
	repo := store.New(migratedPool(t))

	org, err := repo.CreateOrganization(ctx, repo.Pool(), "Acme Corp", "acme-corp")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	p, err := repo.CreateProject(ctx, repo.Pool(), org.ID, "Acme", "acme")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	env, err := repo.CreateEnvironment(ctx, repo.Pool(), p.ID, "staging")
	if err != nil {
		t.Fatalf("create environment: %v", err)
	}

	gotOrg, err := repo.OrgIDForProject(ctx, repo.Pool(), p.ID)
	if err != nil || gotOrg != org.ID {
		t.Fatalf("org id for project = %v, err=%v, want %v", gotOrg, err, org.ID)
	}

	gotOrgViaEnv, err := repo.OrgIDForEnv(ctx, repo.Pool(), env.ID)
	if err != nil || gotOrgViaEnv != org.ID {
		t.Fatalf("org id for env = %v, err=%v, want %v", gotOrgViaEnv, err, org.ID)
	}

	if _, err := repo.OrgIDForProject(ctx, repo.Pool(), uuid.New()); err != store.ErrNotFound {
		t.Fatalf("org id for missing project err = %v, want store.ErrNotFound", err)
	}
	if _, err := repo.OrgIDForEnv(ctx, repo.Pool(), uuid.New()); err != store.ErrNotFound {
		t.Fatalf("org id for missing env err = %v, want store.ErrNotFound", err)
	}
}

// TestListProjectsForUser seeds two organizations, each with its own
// project, and a user who is a member of only one of them. The listing must
// return only that org's project.
func TestListProjectsForUser(t *testing.T) {
	ctx := context.Background()
	repo := store.New(migratedPool(t))

	orgA, err := repo.CreateOrganization(ctx, repo.Pool(), "Org A", "org-a")
	if err != nil {
		t.Fatalf("create org a: %v", err)
	}
	orgB, err := repo.CreateOrganization(ctx, repo.Pool(), "Org B", "org-b")
	if err != nil {
		t.Fatalf("create org b: %v", err)
	}

	projA, err := repo.CreateProject(ctx, repo.Pool(), orgA.ID, "Proj A", "proj-a")
	if err != nil {
		t.Fatalf("create project a: %v", err)
	}
	if _, err := repo.CreateProject(ctx, repo.Pool(), orgB.ID, "Proj B", "proj-b"); err != nil {
		t.Fatalf("create project b: %v", err)
	}

	user, err := repo.CreateUser(ctx, repo.Pool(), "user@acme.test", "hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.AddMember(ctx, repo.Pool(), orgA.ID, user.ID, "viewer"); err != nil {
		t.Fatalf("add member: %v", err)
	}

	list, err := repo.ListProjectsForUser(ctx, repo.Pool(), user.ID)
	if err != nil {
		t.Fatalf("list projects for user: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("list projects for user len = %d, want 1", len(list))
	}
	if list[0].ID != projA.ID {
		t.Fatalf("list[0].ID = %v, want %v", list[0].ID, projA.ID)
	}
}
