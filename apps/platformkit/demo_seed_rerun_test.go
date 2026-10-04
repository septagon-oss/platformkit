package main

// The demo half is applied by the tenant's creation hook with nobody on the
// context, so its tasks arrive unassigned; an operator's later run, as a person
// who holds every grant, assigns them. After that, the brief's idempotence rule
// holds for the tasks and the one uploaded file as for pages: a further run
// creates nothing and updates nothing, and the tenant still holds three seeded
// tasks and one seeded file — the task writer's natural key (title) differs from
// its records' keys, and the file writer has none, so this rests on seed_keys.

import (
	"context"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/seed"
	filecontracts "github.com/septagon-oss/platformkit/modules/file/contracts"
	taskcontracts "github.com/septagon-oss/platformkit/modules/task/contracts"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
)

func TestDemoSeedRerunConvergesOnWorkAndFiles(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	cfg.Demo.Password = "a demo password long enough for the policy"
	c := compose(cfg)
	service, err := seedService(c)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	const operator = "operator@demo-rerun.localhost"
	var assign, again seed.Plan
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, system db.Tx[db.System]) error {
		roles, err := rerunOperatorRoles(ctx, c, system)
		if err != nil {
			return err
		}
		created, err := c.tenants.Create(ctx, system, tenantcontracts.NewTenant{
			Slug: "demo-rerun", Name: "Demo", Host: "demo-rerun.localhost", Demo: true,
		})
		if err != nil {
			return err
		}
		if _, err := c.users.Provision(ctx, system, created.ID, operator, "Operator", "an operator's own long credential", roles); err != nil {
			return err
		}
		return db.InTenant(ctx, system, created.Tenancy(), func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			if ctx, err = seedActor(ctx, c.users, tx, operator); err != nil {
				return err
			}
			if assign, err = service.Apply(ctx, tx, seed.Selection{Demo: true}); err != nil {
				return err
			}
			if again, err = service.Apply(ctx, tx, seed.Selection{Demo: true}); err != nil {
				return err
			}
			tasks, _, err := crud.List[*taskcontracts.Task](tx, crud.Query{Limit: 50})
			if err != nil {
				return err
			}
			if len(tasks) != 3 {
				t.Errorf("demo tenant holds %d tasks after three runs; want 3", len(tasks))
			}
			assigned := 0
			for _, task := range tasks {
				if task.AssigneeID != nil {
					assigned++
				}
			}
			if assigned != 2 {
				t.Errorf("%d demo tasks are assigned after an operator's run; want the 2 the file names", assigned)
			}
			files, _, err := crud.List[*filecontracts.File](tx, crud.Query{Limit: 50})
			if err != nil {
				return err
			}
			if len(files) != 1 {
				t.Errorf("demo tenant holds %d files after three runs; want 1", len(files))
			}
			return nil
		})
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(again.String(), "0 created, 0 updated") {
		t.Errorf("a rerun after the operator's run planned:\n%s\nwant 0 created, 0 updated (the operator's run planned:\n%s)", again.String(), assign.String())
	}
}

// rerunOperatorRoles is the installation administrator's roles, given to the
// demo tenant's operator so the run holds every grant the files ask for.
func rerunOperatorRoles(ctx context.Context, c composition, system db.Tx[db.System]) ([]string, error) {
	var roles []string
	tenant, err := c.tenants.ByHost(ctx, system, acmeHost)
	if err != nil {
		return nil, err
	}
	err = db.InTenant(ctx, system, tenant, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		admin, err := c.users.ByEmail(ctx, tx, adminEmail)
		if err != nil {
			return err
		}
		roles = []string(admin.Roles)
		return nil
	})
	return roles, err
}
