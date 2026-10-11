package main

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/seed"
	taskcontracts "github.com/septagon-oss/platformkit/modules/task/contracts"
)

func TestEditingASeededTaskTitleConverges(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	const file = "seed/starter/tasks.yaml"
	files := fstest.MapFS{file: {Data: []byte("apiVersion: platformkit.seed/v1\nresource: tasks\nrecords:\n  - key: tour\n    fields: {title: Original title, priority: normal}\n")}}
	service, err := seed.New(seed.Deps{Files: files, Root: "seed", Clock: seedClock{},
		Writers: []seed.Writer{taskSeeder{svc: c.tasks}}, Authorize: seedGrants{auth: c.auth}})
	if err != nil {
		t.Fatal(err)
	}
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
			first, err := service.Apply(ctx, tx, seed.Selection{})
			if err != nil {
				return err
			}
			files[file].Data = []byte(strings.ReplaceAll(string(files[file].Data), "Original title", "Edited title"))
			if _, err := service.Apply(ctx, tx, seed.Selection{}); err != nil {
				return err
			}
			row, err := crud.Get[*taskcontracts.Task](tx, first.Items[0].RecordID)
			if err != nil {
				return err
			}
			if row.Title != "Edited title" {
				t.Errorf("updated task title = %q; want Edited title on the same row", row.Title)
			}
			again, err := service.Apply(ctx, tx, seed.Selection{})
			if err != nil {
				return err
			}
			if !strings.Contains(again.String(), "0 created, 0 updated, 1 unchanged") {
				t.Errorf("an already-applied title edit did not converge: %s", again)
			}
			return nil
		})
	}); err != nil {
		t.Fatal(err)
	}
}
