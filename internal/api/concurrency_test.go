//go:build integration

package api_test

import (
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Rohan-Muslekar/knobs/internal/schema"
)

// TestSchemaValueConcurrencyInvariant guards the concurrency race fixed in
// Task 1: handlePutValues used to read the project's schema outside its tx,
// and handlePutSchema's change-safety check used to read each environment's
// live values outside its tx. With no lock tying the two together, a
// schema-tightening PUT and a values PUT could interleave and commit in an
// order that leaves environment.current_version pointing at values that
// violate the now-current schema.
//
// Exact interleaving is timing-dependent, so this isn't a single
// reproduction of the race — it's an invariant checked over many iterations:
// whatever order the two concurrent writes actually land in, the
// environment's current values must always validate against the current
// schema afterwards. Pre-fix this could fail (rarely, given how tight the
// window is); post-fix it must never fail, because both handlers now hold
// the same config_schema row lock and so can never observe each other's
// half-committed state.
func TestSchemaValueConcurrencyInvariant(t *testing.T) {
	router, cookie, repo := seededRouter(t)

	projRec := post(router, "/v1/projects", `{"name":"Acme","slug":"acme"}`, cookie)
	if projRec.Code != http.StatusCreated {
		t.Fatalf("create project = %d, want 201, body=%s", projRec.Code, projRec.Body.String())
	}
	var proj map[string]any
	_ = json.NewDecoder(projRec.Body).Decode(&proj)
	projectIDStr, _ := proj["id"].(string)
	if projectIDStr == "" {
		t.Fatal("no project id returned")
	}
	projectID, err := uuid.Parse(projectIDStr)
	if err != nil {
		t.Fatalf("parse project id: %v", err)
	}

	wideSchema := `{"fields":[{"name":"maxRetries","type":"int","required":true,"max":5}]}`
	if rec := put(router, "/v1/projects/"+projectIDStr+"/schema", wideSchema, cookie); rec.Code != http.StatusOK {
		t.Fatalf("seed schema = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	envRec := post(router, "/v1/projects/"+projectIDStr+"/environments", `{"name":"staging"}`, cookie)
	if envRec.Code != http.StatusCreated {
		t.Fatalf("create env = %d, want 201, body=%s", envRec.Code, envRec.Body.String())
	}
	var env map[string]any
	_ = json.NewDecoder(envRec.Body).Decode(&env)
	envIDStr, _ := env["id"].(string)
	if envIDStr == "" {
		t.Fatal("no environment id returned")
	}
	envID, err := uuid.Parse(envIDStr)
	if err != nil {
		t.Fatalf("parse env id: %v", err)
	}

	valuesAt5 := `{"values":{"maxRetries":5}}`
	if rec := put(router, "/v1/environments/"+envIDStr+"/values", valuesAt5, cookie); rec.Code != http.StatusOK {
		t.Fatalf("seed values = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	tightSchema := `{"fields":[{"name":"maxRetries","type":"int","required":true,"max":2}]}`

	const iterations = 20
	for i := 0; i < iterations; i++ {
		// Reset to the known-consistent starting state before racing again:
		// schema max back to 5, values back to maxRetries:5. Sequential, no
		// race here — these two must both land before the next race starts.
		if rec := put(router, "/v1/projects/"+projectIDStr+"/schema", wideSchema, cookie); rec.Code != http.StatusOK {
			t.Fatalf("iter %d: reset schema = %d, want 200, body=%s", i, rec.Code, rec.Body.String())
		}
		if rec := put(router, "/v1/environments/"+envIDStr+"/values", valuesAt5, cookie); rec.Code != http.StatusOK {
			t.Fatalf("iter %d: reset values = %d, want 200, body=%s", i, rec.Code, rec.Body.String())
		}

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			put(router, "/v1/projects/"+projectIDStr+"/schema", tightSchema, cookie)
		}()
		go func() {
			defer wg.Done()
			put(router, "/v1/environments/"+envIDStr+"/values", valuesAt5, cookie)
		}()

		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatalf("iter %d: concurrent schema/value writes did not settle within timeout", i)
		}

		// Load the current schema and current values fresh from the repo
		// (not the two HTTP responses above, which each only reflect one
		// side's own outcome) and assert the invariant.
		cs, err := repo.GetOrInitSchema(t.Context(), repo.Pool(), projectID)
		if err != nil {
			t.Fatalf("iter %d: load current schema: %v", i, err)
		}
		cv, err := repo.CurrentVersion(t.Context(), repo.Pool(), envID)
		if err != nil {
			t.Fatalf("iter %d: load current values: %v", i, err)
		}
		compiled, err := schema.Compile(cs.Definition)
		if err != nil {
			t.Fatalf("iter %d: compile current schema: %v", i, err)
		}
		if err := schema.ValidateValues(compiled, cv.Values); err != nil {
			t.Fatalf("iter %d: INVARIANT VIOLATED: current values %v (version %d) invalid against current schema (schemaVersion %d): %v",
				i, cv.Values, cv.Version, cs.SchemaVersion, err)
		}
	}
}
