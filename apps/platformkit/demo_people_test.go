package main

import (
	"context"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	filecontracts "github.com/septagon-oss/platformkit/modules/file/contracts"
	taskcontracts "github.com/septagon-oss/platformkit/modules/task/contracts"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
)

// Demo people can follow a walkthrough immediately after the demo seed runs.
// Their roles and password come through the user owner's commands, not from
// fields the seed inserts directly into the users table.
func TestDemoPeopleHaveTheirRolesAndPassword(t *testing.T) {
	// The demo credential is a deployment's answer, and it has to be named before
	// the configuration is read, because the people it names arrive with the
	// tenant. A demo tenant created while the deployment named no password gets
	// people the seed minted a credential for, and a rerun never overwrites the
	// credential a row already holds — which is what
	// TestSeedRerunPreservesAPersonsOwnPassword exists to prove. Every assertion
	// below is unchanged by saying the password first: the three people still sign
	// in with this one.
	const password = "demo walkthrough password 2026"
	t.Setenv("PLATFORMKIT_DEMO_PASSWORD", password)
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, system db.Tx[db.System]) error {
		created, err := c.tenants.Create(ctx, system, tenantcontracts.NewTenant{
			Slug: "demo", Name: "Demo", Host: "demo.localhost", Demo: true,
		})
		if err != nil {
			return err
		}
		_, err = c.users.Provision(ctx, system, created.ID, "root@demo.localhost", "Root",
			adminPass, []string{authcontracts.RoleAdmin})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := seedCommand([]string{"--config", path, "--tenant", "demo", "--as", "root@demo.localhost", "--demo"}); err != nil {
		t.Fatalf("seed the demo tenant: %v", err)
	}

	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, system db.Tx[db.System]) error {
		tenant, err := c.tenants.ByHost(ctx, system, "demo.localhost")
		if err != nil {
			return err
		}
		return db.InTenant(ctx, system, tenant, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			for _, email := range []string{"marta@example.test", "devin@example.test", "amara@example.test"} {
				person, err := c.users.ByEmail(ctx, tx, email)
				if err != nil {
					t.Errorf("seeded person %s: %v", email, err)
					continue
				}
				if len(person.Roles) == 0 {
					t.Errorf("seeded person %s has no role for the demo journey", email)
				}
				if !person.CanSignIn() || !person.CheckPassword(password) {
					t.Errorf("seeded person %s cannot sign in with the demo password", email)
				}
			}
			// The work the demo files declare, and the picture one page carries.
			// The tenant's own creation had no person to put in front of the task
			// module's policy, so it wrote the tasks unassigned; this run named one,
			// so the assignments the file declares are here now — which is what a
			// reconciliation run is for.
			tasks, taskCount, err := crud.List[*taskcontracts.Task](tx, crud.Query{Limit: 10})
			if err != nil {
				return err
			}
			if taskCount != 3 {
				t.Errorf("the demo seed wrote %d tasks, want the three its file declares", taskCount)
			}
			var assigned int
			for _, task := range tasks {
				if task.AssigneeID != nil {
					assigned++
				}
			}
			if assigned != 2 {
				t.Errorf("%d of the demo tasks carry an assignee after a run that named a person, want the two its file names", assigned)
			}
			images, imageCount, err := crud.List[*filecontracts.File](tx, crud.Query{Limit: 10})
			if err != nil {
				return err
			}
			if imageCount != 1 || images[0].Name != "welcome.png" || !images[0].Public() || images[0].Size == 0 {
				t.Errorf("the demo seed's embedded asset is not the public, non-empty upload its record names: %+v", images)
			}
			return nil
		})
	}); err != nil {
		t.Fatal(err)
	}
}
