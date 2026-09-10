//go:build integration

package api

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/Rohan-Muslekar/knobs/internal/authz"
	"github.com/Rohan-Muslekar/knobs/internal/store"
)

// migratedPool starts a throwaway Postgres, applies all migrations, and
// returns a live pgx pool. The container is terminated on test cleanup.
//
// Identical implementation to internal/api/testhelper_test.go's version;
// duplicated here because that one lives in package api_test and this file
// needs package api to reach the unexported authorize* helpers, and test
// helpers do not cross package boundaries.
func migratedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	pg, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("knobs"),
		tcpostgres.WithUsername("knobs"),
		tcpostgres.WithPassword("knobs"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(pg) })

	url, err := pg.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connstring: %v", err)
	}

	sqlDB, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := store.RunMigrations(sqlDB); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	_ = sqlDB.Close()

	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// authzFixture seeds two organizations for the authorize* tests: org A has
// one member per role plus a project and environment; org B exists only to
// hold the outsider, who is never added to org A.
type authzFixture struct {
	repo *store.Repo

	orgA, orgB store.Organization
	project    store.Project
	env        store.Environment
	viewer     store.User
	editor     store.User
	admin      store.User
	owner      store.User
	outsider   store.User
}

func seedAuthzFixture(t *testing.T) authzFixture {
	t.Helper()
	ctx := context.Background()
	repo := store.New(migratedPool(t))
	db := repo.Pool()

	orgA, err := repo.CreateOrganization(ctx, db, "Org A", "org-a")
	if err != nil {
		t.Fatalf("create org a: %v", err)
	}
	orgB, err := repo.CreateOrganization(ctx, db, "Org B", "org-b")
	if err != nil {
		t.Fatalf("create org b: %v", err)
	}

	project, err := repo.CreateProject(ctx, db, orgA.ID, "Acme", "acme")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	env, err := repo.CreateEnvironment(ctx, db, project.ID, "staging")
	if err != nil {
		t.Fatalf("create environment: %v", err)
	}

	mkUser := func(email, role string, org store.Organization) store.User {
		u, err := repo.CreateUser(ctx, db, email, "hash")
		if err != nil {
			t.Fatalf("create user %s: %v", email, err)
		}
		if role != "" {
			if err := repo.AddMember(ctx, db, org.ID, u.ID, role); err != nil {
				t.Fatalf("add member %s: %v", email, err)
			}
		}
		return u
	}

	return authzFixture{
		repo:     repo,
		orgA:     orgA,
		orgB:     orgB,
		project:  project,
		env:      env,
		viewer:   mkUser("viewer@a.test", "viewer", orgA),
		editor:   mkUser("editor@a.test", "editor", orgA),
		admin:    mkUser("admin@a.test", "admin", orgA),
		owner:    mkUser("owner@a.test", "owner", orgA),
		outsider: mkUser("outsider@b.test", "viewer", orgB),
	}
}

func TestAuthorizeOrg(t *testing.T) {
	fx := seedAuthzFixture(t)
	d := Deps{Repo: fx.repo}
	ctx := context.Background()
	db := fx.repo.Pool()

	// A member whose role meets the minimum gets their role back, no error.
	role, err := d.authorizeOrg(ctx, db, fx.editor.ID, fx.orgA.ID, authz.RoleEditor)
	if err != nil {
		t.Fatalf("editor >= editor: unexpected err %v", err)
	}
	if role != authz.RoleEditor {
		t.Fatalf("editor >= editor: role = %q, want editor", role)
	}

	// A member whose role falls short gets errForbidden (403).
	_, err = d.authorizeOrg(ctx, db, fx.viewer.ID, fx.orgA.ID, authz.RoleAdmin)
	if !errors.Is(err, errForbidden) {
		t.Fatalf("viewer >= admin: err = %v, want errForbidden", err)
	}

	// A non-member gets store.ErrNotFound (404), existence hidden.
	_, err = d.authorizeOrg(ctx, db, fx.outsider.ID, fx.orgA.ID, authz.RoleViewer)
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("outsider: err = %v, want store.ErrNotFound", err)
	}
}

func TestAuthorizeProject(t *testing.T) {
	fx := seedAuthzFixture(t)
	d := Deps{Repo: fx.repo}
	ctx := context.Background()
	db := fx.repo.Pool()

	// Sufficiently-privileged member: gets the project row and role back.
	p, role, err := d.authorizeProject(ctx, db, fx.admin.ID, fx.project.ID, authz.RoleAdmin)
	if err != nil {
		t.Fatalf("admin >= admin: unexpected err %v", err)
	}
	if p.ID != fx.project.ID {
		t.Fatalf("project id = %v, want %v", p.ID, fx.project.ID)
	}
	if role != authz.RoleAdmin {
		t.Fatalf("role = %q, want admin", role)
	}

	// Member with too-low a role: 403.
	_, _, err = d.authorizeProject(ctx, db, fx.viewer.ID, fx.project.ID, authz.RoleAdmin)
	if !errors.Is(err, errForbidden) {
		t.Fatalf("viewer >= admin: err = %v, want errForbidden", err)
	}

	// Non-member (outsider from org B): 404, existence hidden.
	_, _, err = d.authorizeProject(ctx, db, fx.outsider.ID, fx.project.ID, authz.RoleViewer)
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("outsider: err = %v, want store.ErrNotFound", err)
	}

	// Genuinely missing project: 404.
	_, _, err = d.authorizeProject(ctx, db, fx.owner.ID, uuid.New(), authz.RoleViewer)
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing project: err = %v, want store.ErrNotFound", err)
	}
}

func TestAuthorizeEnv(t *testing.T) {
	fx := seedAuthzFixture(t)
	d := Deps{Repo: fx.repo}
	ctx := context.Background()
	db := fx.repo.Pool()

	// Sufficiently-privileged member: gets the environment row and role back.
	e, role, err := d.authorizeEnv(ctx, db, fx.owner.ID, fx.env.ID, authz.RoleOwner)
	if err != nil {
		t.Fatalf("owner >= owner: unexpected err %v", err)
	}
	if e.ID != fx.env.ID {
		t.Fatalf("env id = %v, want %v", e.ID, fx.env.ID)
	}
	if role != authz.RoleOwner {
		t.Fatalf("role = %q, want owner", role)
	}

	// Member with too-low a role: 403.
	_, _, err = d.authorizeEnv(ctx, db, fx.editor.ID, fx.env.ID, authz.RoleOwner)
	if !errors.Is(err, errForbidden) {
		t.Fatalf("editor >= owner: err = %v, want errForbidden", err)
	}

	// Non-member (outsider from org B): 404, existence hidden.
	_, _, err = d.authorizeEnv(ctx, db, fx.outsider.ID, fx.env.ID, authz.RoleViewer)
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("outsider: err = %v, want store.ErrNotFound", err)
	}

	// Genuinely missing environment: 404.
	_, _, err = d.authorizeEnv(ctx, db, fx.owner.ID, uuid.New(), authz.RoleViewer)
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing env: err = %v, want store.ErrNotFound", err)
	}
}
