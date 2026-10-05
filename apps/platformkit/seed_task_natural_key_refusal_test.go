package main

// The task writer is named by a field it does not key on: it declares `title` as the
// natural key, and a seed file's keys are seed names, not the titles they declare
// (`take-the-tour` names the task "Take the tour of the site"). The read therefore
// meets a row by `title = key`, so the only row it can meet is somebody else's — a
// person who titled their own work exactly like a seed key.
//
// What that run does is the point of this case. It refuses, because the row it found
// carries no provenance and the file's title differs from the row's, so the write
// would be an update of a person's own task; and a refused run writes nothing, so the
// person's row keeps its title and priority, no provenance is claimed for it, and the
// record the file declared is simply not written. The seed does not adopt, edit or
// remove a task it did not write, whichever way the keys fall.

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/seed"
	"github.com/septagon-oss/platformkit/modules/task"
	taskcontracts "github.com/septagon-oss/platformkit/modules/task/contracts"
)

func TestATaskTitledLikeASeedKeyRefusesTheRunAndWritesNothing(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	const file = "seed/starter/tasks.yaml"
	service, err := seed.New(seed.Deps{
		Files: fstest.MapFS{file: {Data: []byte("apiVersion: platformkit.seed/v1\nresource: tasks\nrecords:\n  - key: invite-a-colleague\n    fields: {title: Invite a colleague and give them a role, priority: low}\n")}},
		Root:  "seed", Clock: seedClock{},
		Writers: []seed.Writer{taskSeeder{svc: c.tasks}}, Authorize: seedGrants{auth: c.auth},
	})
	if err != nil {
		t.Fatal(err)
	}
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	var refusal error
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, system db.Tx[db.System]) error {
		tenant, err := c.tenants.ByHost(ctx, system, acmeHost)
		if err != nil {
			return err
		}
		return db.InTenant(ctx, system, tenant, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			ctx, err = seedActor(ctx, c.users, tx, adminEmail)
			if err != nil {
				return err
			}
			own, err := task.Spec.CreateRow(ctx, tx, &taskcontracts.Task{
				Title: "invite-a-colleague", Priority: taskcontracts.PriorityLow,
			})
			if err != nil {
				return err
			}
			_, refusal = service.Apply(ctx, tx, seed.Selection{})
			if refusal == nil {
				t.Error("a run that met a person's own task by title wrote it; want a refusal")
			} else if !strings.Contains(refusal.Error(), "unowned natural-key row") {
				t.Errorf("refusal was %v; want it to name the row it does not own", refusal)
			} else if !strings.Contains(refusal.Error(), file+":4:5") {
				t.Errorf("refusal %v does not say which declaration caused it", refusal)
			}
			var claimed int
			if err := tx.DB().Raw(`SELECT count(*) FROM seed_keys WHERE module = 'task'`).Row().Scan(&claimed); err != nil {
				return err
			}
			if claimed != 0 {
				t.Errorf("%d task provenance rows after a refused run; want the refusal to claim nothing", claimed)
			}
			row, err := crud.Get[*taskcontracts.Task](tx, own.ID)
			if err != nil {
				return err
			}
			if row.Title != "invite-a-colleague" || row.Priority != taskcontracts.PriorityLow {
				t.Errorf("the person's task came out %q/%s; want it untouched at %q/%s",
					row.Title, row.Priority, "invite-a-colleague", taskcontracts.PriorityLow)
			}
			tasks, total, err := crud.List[*taskcontracts.Task](tx, crud.Query{Limit: 50})
			if err != nil {
				return err
			}
			if total != 1 {
				t.Errorf("%d tasks held after the refused run; want only the person's own", total)
			}
			for _, one := range tasks {
				if one.Title == "Invite a colleague and give them a role" {
					t.Error("the record the refused run declared was written anyway")
				}
			}
			return nil
		})
	}); err != nil {
		t.Fatal(err)
	}
}
