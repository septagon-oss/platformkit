package main

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/seed"
	contentcontracts "github.com/septagon-oss/platformkit/modules/content/contracts"
)

// TestTwoSeedRunsOnOneTenantCreateEachRecordOnce overlaps two seed runs of the
// same files on one tenant, through the composition's real owners. The first run
// applies and holds its transaction open; the second starts while it is open.
// The per-tenant run lock is what makes the second wait and then reconcile against
// the committed rows: both runs succeed, the record is created once, and the
// second run reports it unchanged. Without the lock the second run reads the row
// as absent, asks the owner to create it, and fails on the slug's unique index.
func TestTwoSeedRunsOnOneTenantCreateEachRecordOnce(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	files := fstest.MapFS{
		"seed/starter/contents.yaml": {Data: []byte(`apiVersion: platformkit.seed/v1
resource: contents
records:
  - key: both-runs
    fields: {kind: page, title: Both runs, body: "Written once."}
`)},
	}
	service, err := seed.New(seed.Deps{
		Files: files, Root: "seed", Clock: seedClock{},
		Writers:   []seed.Writer{&contentSeeder{svc: c.contents}},
		Authorize: seedGrants{auth: c.auth},
	})
	if err != nil {
		t.Fatalf("the seed this case writes with: %v", err)
	}
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	run := func(applied chan<- struct{}, hold time.Duration) (seed.Plan, error) {
		var plan seed.Plan
		err := dbtest.System(t.Context(), conn, func(ctx context.Context, system db.Tx[db.System]) error {
			tenant, err := c.tenants.ByHost(ctx, system, acmeHost)
			if err != nil {
				return err
			}
			return db.InTenant(ctx, system, tenant, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				if ctx, err = seedActor(ctx, c.users, tx, adminEmail); err != nil {
					return err
				}
				if plan, err = service.Apply(ctx, tx, seed.Selection{}); err != nil {
					return err
				}
				if applied != nil {
					close(applied)
					time.Sleep(hold)
				}
				return nil
			})
		})
		return plan, err
	}

	applied := make(chan struct{})
	type result struct {
		plan seed.Plan
		err  error
	}
	first := make(chan result, 1)
	go func() {
		plan, err := run(applied, 500*time.Millisecond)
		first <- result{plan, err}
	}()
	select {
	case <-applied:
	case r := <-first:
		t.Fatalf("the first run ended before it applied: %v", r.err)
	}
	secondPlan, secondErr := run(nil, 0)
	r := <-first

	if r.err != nil {
		t.Fatalf("the first run: %v", r.err)
	}
	if secondErr != nil {
		t.Fatalf("the second run, started while the first was open, failed instead of waiting for it: %v", secondErr)
	}
	if !strings.Contains(r.plan.String(), "1 created") {
		t.Errorf("the first run's plan: %s", r.plan)
	}
	if !strings.Contains(secondPlan.String(), "0 created, 0 updated, 1 unchanged") {
		t.Errorf("the second run's plan: %s; it reconciles against the first run's committed row", secondPlan)
	}
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, system db.Tx[db.System]) error {
		tenant, err := c.tenants.ByHost(ctx, system, acmeHost)
		if err != nil {
			return err
		}
		return db.InTenant(ctx, system, tenant, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			_, n, err := crud.List[*contentcontracts.Content](tx, crud.Query{Limit: 5, Filter: map[string]any{"slug": "both-runs"}})
			if err != nil {
				return err
			}
			if n != 1 {
				t.Errorf("both-runs exists %d times; two runs of one file create it once", n)
			}
			return nil
		})
	}); err != nil {
		t.Fatal(err)
	}
}
