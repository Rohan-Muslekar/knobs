//go:build integration

package store_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/google/uuid"

	"github.com/Rohan-Muslekar/knobs/internal/store"
	"github.com/Rohan-Muslekar/knobs/internal/targeting"
)

func TestVersionsLifecycle(t *testing.T) {
	ctx := context.Background()
	repo := store.New(migratedPool(t))
	org := defaultOrgID(t, ctx, repo)

	p, err := repo.CreateProject(ctx, repo.Pool(), org, "Acme", "acme")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	env, err := repo.CreateEnvironment(ctx, repo.Pool(), p.ID, "staging")
	if err != nil {
		t.Fatalf("create environment: %v", err)
	}
	if _, err := repo.GetOrInitSchema(ctx, repo.Pool(), p.ID); err != nil {
		t.Fatalf("init schema: %v", err)
	}

	n, err := repo.NextVersionNumber(ctx, repo.Pool(), env.ID)
	if err != nil {
		t.Fatalf("next version (fresh env): %v", err)
	}
	if n != 1 {
		t.Fatalf("next version (fresh env) = %d, want 1", n)
	}

	v1, err := repo.InsertVersion(ctx, repo.Pool(), env.ID, n, 1, map[string]any{"maxRetries": float64(3)}, nil, uuid.Nil)
	if err != nil {
		t.Fatalf("insert v1: %v", err)
	}
	if v1.Version != 1 {
		t.Fatalf("v1.Version = %d, want 1", v1.Version)
	}
	if v1.CreatedBy != nil {
		t.Fatalf("v1.CreatedBy = %v, want nil (createdBy passed as uuid.Nil)", v1.CreatedBy)
	}
	rev1, err := repo.SetCurrentVersion(ctx, repo.Pool(), env.ID, v1.ID)
	if err != nil {
		t.Fatalf("set current v1: %v", err)
	}
	if rev1 != 1 {
		t.Fatalf("revision after first SetCurrentVersion = %d, want 1", rev1)
	}

	cur, err := repo.CurrentVersion(ctx, repo.Pool(), env.ID)
	if err != nil {
		t.Fatalf("current version: %v", err)
	}
	if cur.Version != 1 {
		t.Fatalf("current.Version = %d, want 1", cur.Version)
	}
	if got, _ := cur.Values["maxRetries"].(float64); got != 3 {
		t.Fatalf("current.Values[maxRetries] = %v, want 3", cur.Values["maxRetries"])
	}

	n2, err := repo.NextVersionNumber(ctx, repo.Pool(), env.ID)
	if err != nil {
		t.Fatalf("next version (after v1): %v", err)
	}
	if n2 != 2 {
		t.Fatalf("next version (after v1) = %d, want 2", n2)
	}

	v2, err := repo.InsertVersion(ctx, repo.Pool(), env.ID, n2, 1, map[string]any{"maxRetries": float64(4)}, nil, uuid.Nil)
	if err != nil {
		t.Fatalf("insert v2: %v", err)
	}
	rev2, err := repo.SetCurrentVersion(ctx, repo.Pool(), env.ID, v2.ID)
	if err != nil {
		t.Fatalf("set current v2: %v", err)
	}
	if rev2 <= rev1 {
		t.Fatalf("revision after second SetCurrentVersion = %d, want > %d", rev2, rev1)
	}

	// History is immutable: version 1 must still return its original values
	// even after the environment's current pointer has moved to v2.
	old, err := repo.VersionByNumber(ctx, repo.Pool(), env.ID, 1)
	if err != nil {
		t.Fatalf("version by number (1): %v", err)
	}
	if got, _ := old.Values["maxRetries"].(float64); got != 3 {
		t.Fatalf("version 1 values[maxRetries] = %v, want 3 (unchanged by v2)", old.Values["maxRetries"])
	}

	if _, err := repo.VersionByNumber(ctx, repo.Pool(), env.ID, 99); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("version by number (missing) err = %v, want ErrNotFound", err)
	}

	n3, err := repo.NextVersionNumber(ctx, repo.Pool(), env.ID)
	if err != nil {
		t.Fatalf("next version (after v2): %v", err)
	}
	if n3 != 3 {
		t.Fatalf("next version (after v2) = %d, want 3", n3)
	}

	// ListVersions returns both versions, most recent first, with IsCurrent
	// true only on v2 (the environment's current pointer).
	versions, err := repo.ListVersions(ctx, repo.Pool(), env.ID)
	if err != nil {
		t.Fatalf("list versions: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("list versions len = %d, want 2", len(versions))
	}
	if versions[0].Version != 2 || !versions[0].IsCurrent {
		t.Fatalf("versions[0] = %+v, want version 2, current", versions[0])
	}
	if versions[1].Version != 1 || versions[1].IsCurrent {
		t.Fatalf("versions[1] = %+v, want version 1, not current", versions[1])
	}
	if versions[0].SchemaVersion != 1 {
		t.Fatalf("versions[0].SchemaVersion = %d, want 1", versions[0].SchemaVersion)
	}

	// A rollback (repository-level: just SetCurrentVersion) flips IsCurrent
	// back to v1 without touching either version's stored history. This is
	// the core regression case for the monotonic delivery revision: the
	// user-facing version goes backwards (2 -> 1), but delivery_revision
	// must keep climbing — SetCurrentVersion bumps it unconditionally on
	// every call, rollback included, precisely so a rollback is never
	// ambiguous on the revision axis.
	revRollback, err := repo.SetCurrentVersion(ctx, repo.Pool(), env.ID, v1.ID)
	if err != nil {
		t.Fatalf("set current v1 (rollback): %v", err)
	}
	if revRollback <= rev2 {
		t.Fatalf("revision after rollback = %d, want > %d (revision must keep increasing even though version went 2 -> 1)", revRollback, rev2)
	}

	envAfterRollback, err := repo.EnvironmentByID(ctx, repo.Pool(), env.ID)
	if err != nil {
		t.Fatalf("environment by id after rollback: %v", err)
	}
	if envAfterRollback.DeliveryRevision != revRollback {
		t.Fatalf("environment.DeliveryRevision = %d, want %d (matching SetCurrentVersion's returned revision)", envAfterRollback.DeliveryRevision, revRollback)
	}

	versionsAfterRollback, err := repo.ListVersions(ctx, repo.Pool(), env.ID)
	if err != nil {
		t.Fatalf("list versions after rollback: %v", err)
	}
	if len(versionsAfterRollback) != 2 {
		t.Fatalf("list versions after rollback len = %d, want 2", len(versionsAfterRollback))
	}
	if versionsAfterRollback[0].Version != 2 || versionsAfterRollback[0].IsCurrent {
		t.Fatalf("versionsAfterRollback[0] = %+v, want version 2, not current", versionsAfterRollback[0])
	}
	if versionsAfterRollback[1].Version != 1 || !versionsAfterRollback[1].IsCurrent {
		t.Fatalf("versionsAfterRollback[1] = %+v, want version 1, current", versionsAfterRollback[1])
	}
}

// TestVersionTargetingRoundTrip verifies targeting rules stored on a
// config_version survive the jsonb round trip through both read paths:
// CurrentVersion (joined through environment.current_version_id) and
// VersionByNumber (read directly by environment+version).
func TestVersionTargetingRoundTrip(t *testing.T) {
	ctx := context.Background()
	repo := store.New(migratedPool(t))
	org := defaultOrgID(t, ctx, repo)

	p, err := repo.CreateProject(ctx, repo.Pool(), org, "Acme", "acme")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	env, err := repo.CreateEnvironment(ctx, repo.Pool(), p.ID, "staging")
	if err != nil {
		t.Fatalf("create environment: %v", err)
	}
	if _, err := repo.GetOrInitSchema(ctx, repo.Pool(), p.ID); err != nil {
		t.Fatalf("init schema: %v", err)
	}

	n, err := repo.NextVersionNumber(ctx, repo.Pool(), env.ID)
	if err != nil {
		t.Fatalf("next version: %v", err)
	}

	tgt := targeting.Map{
		"maxRetries": []targeting.Rule{
			{
				Conditions: []targeting.Condition{
					{Attribute: "plan", Operator: targeting.OpEq, Values: []any{"enterprise"}},
				},
				Value: float64(10),
			},
			{
				Rollout: &targeting.Rollout{
					Salt: "maxRetries",
					Variants: []targeting.Variant{
						{Value: float64(3), Weight: 50},
						{Value: float64(5), Weight: 50},
					},
				},
			},
		},
	}

	v1, err := repo.InsertVersion(ctx, repo.Pool(), env.ID, n, 1, map[string]any{"maxRetries": float64(3)}, tgt, uuid.Nil)
	if err != nil {
		t.Fatalf("insert version with targeting: %v", err)
	}
	if !reflect.DeepEqual(v1.Targeting, tgt) {
		t.Fatalf("InsertVersion returned targeting = %+v, want %+v", v1.Targeting, tgt)
	}

	if _, err := repo.SetCurrentVersion(ctx, repo.Pool(), env.ID, v1.ID); err != nil {
		t.Fatalf("set current: %v", err)
	}

	cur, err := repo.CurrentVersion(ctx, repo.Pool(), env.ID)
	if err != nil {
		t.Fatalf("current version: %v", err)
	}
	if !reflect.DeepEqual(cur.Targeting, tgt) {
		t.Fatalf("CurrentVersion targeting = %+v, want %+v", cur.Targeting, tgt)
	}

	byNum, err := repo.VersionByNumber(ctx, repo.Pool(), env.ID, n)
	if err != nil {
		t.Fatalf("version by number: %v", err)
	}
	if !reflect.DeepEqual(byNum.Targeting, tgt) {
		t.Fatalf("VersionByNumber targeting = %+v, want %+v", byNum.Targeting, tgt)
	}

	// An empty/nil targeting map round-trips as an empty (non-nil) Map, not
	// as a JSON null that would fail to unmarshal.
	n2, err := repo.NextVersionNumber(ctx, repo.Pool(), env.ID)
	if err != nil {
		t.Fatalf("next version (2): %v", err)
	}
	v2, err := repo.InsertVersion(ctx, repo.Pool(), env.ID, n2, 1, map[string]any{"maxRetries": float64(4)}, nil, uuid.Nil)
	if err != nil {
		t.Fatalf("insert version without targeting: %v", err)
	}
	if len(v2.Targeting) != 0 {
		t.Fatalf("v2.Targeting = %+v, want empty", v2.Targeting)
	}
}
