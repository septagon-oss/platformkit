package main

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/seed"
	taskcontracts "github.com/septagon-oss/platformkit/modules/task/contracts"
)

func TestRemovingASeedAssigneeConvergesOrRefusesWithoutWriting(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	const file = "seed/starter/tasks.yaml"
	files := fstest.MapFS{file: {Data: []byte(`apiVersion: platformkit.seed/v1
resource: tasks
records:
  - key: owned-assignment
    fields: {title: Owned assignment, assignee: users/admin@acme.test}
`)}}
	service, err := seed.New(seed.Deps{Files: files, Root: "seed", Clock: seedClock{},
		Writers:   []seed.Writer{taskSeeder{svc: c.tasks}, userSeeder{users: c.users}},
		Authorize: seedGrants{auth: c.auth}})
	if err != nil {
		t.Fatal(err)
	}
	var id uuid.UUID
	var original *taskcontracts.Task
	var beforeEvents int
	withTenant := func(fn func(context.Context, db.Tx[db.Tenant]) error) error {
		return dbtest.System(t.Context(), conn, func(ctx context.Context, system db.Tx[db.System]) error {
			tenant, err := c.tenants.ByHost(ctx, system, acmeHost)
			if err != nil {
				return err
			}
			return db.InTenant(ctx, system, tenant, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				ctx, err = seedActor(ctx, c.users, tx, adminEmail)
				if err != nil {
					return err
				}
				return fn(ctx, tx)
			})
		})
	}
	// Use the installation's actual actor address for the existing reference.
	files[file].Data = []byte(strings.ReplaceAll(string(files[file].Data), "admin@acme.test", adminEmail))
	if err := withTenant(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		plan, err := service.Apply(ctx, tx, seed.Selection{})
		if err != nil {
			return err
		}
		if len(plan.Items) != 1 {
			t.Fatalf("first plan = %s; want one task", plan)
		}
		id = plan.Items[0].RecordID
		original, err = crud.Get[*taskcontracts.Task](tx, id)
		if err != nil {
			return err
		}
		if original.AssigneeID == nil {
			t.Fatal("first run did not assign the task")
		}
		return tx.DB().Raw("SELECT count(*) FROM platformkit_outbox").Row().Scan(&beforeEvents)
	}); err != nil {
		t.Fatal(err)
	}
	files[file].Data = []byte(`apiVersion: platformkit.seed/v1
resource: tasks
records:
  - key: owned-assignment
    fields: {title: Owned assignment}
`)
	var result seed.Plan
	applyErr := withTenant(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		result, err = service.Apply(ctx, tx, seed.Selection{})
		return err
	})
	if applyErr != nil && (!strings.Contains(applyErr.Error(), file+":") || len(result.Items) != 0) {
		t.Errorf("refusal = %v, result = %s; want source and no stale result", applyErr, result)
	}
	if err := withTenant(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		row, err := crud.Get[*taskcontracts.Task](tx, id)
		if err != nil {
			return err
		}
		var afterEvents int
		if err := tx.DB().Raw("SELECT count(*) FROM platformkit_outbox").Row().Scan(&afterEvents); err != nil {
			return err
		}
		if applyErr != nil {
			if !row.UpdatedAt.Equal(original.UpdatedAt) || afterEvents != beforeEvents {
				t.Errorf("refused removal changed row or events: before=%d after=%d", beforeEvents, afterEvents)
			}
			return nil
		}
		if row.AssigneeID != nil {
			t.Errorf("successful removal retained assignee %s and emitted %d events", row.AssigneeID, afterEvents-beforeEvents)
		}
		again, err := service.Apply(ctx, tx, seed.Selection{})
		if err != nil {
			return err
		}
		if len(again.Items) != 1 || again.Items[0].Action != seed.Unchanged {
			t.Errorf("removal did not converge: %s", again)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
