package main

import (
	"context"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/seed"
)

// A declared value the owner cannot hold is refused at its line, whatever its
// YAML type: a number where a priority, a kind or a visibility belongs is a bad
// value, never an absent one the writer may fill with its default. The string
// control beside each shows the owner's own refusal is reached.
func TestSeedRefusesAFieldValueOfTheWrongShape(t *testing.T) {
	asset, err := fs.ReadFile(seedFiles, "seed/demo/assets/welcome.png")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, file, document, table string
		writer                      func(composition) seed.Writer
	}{
		{"task priority as a string the owner refuses", "seed/starter/tasks.yaml",
			"resource: tasks\nrecords:\n  - key: shaped-wrong\n    fields: {title: Shaped wrong, priority: whenever}\n",
			"tasks", func(c composition) seed.Writer { return taskSeeder{svc: c.tasks} }},
		{"task priority as a number", "seed/starter/tasks.yaml",
			"resource: tasks\nrecords:\n  - key: shaped-wrong\n    fields: {title: Shaped wrong, priority: 5}\n",
			"tasks", func(c composition) seed.Writer { return taskSeeder{svc: c.tasks} }},
		{"page kind as a number", "seed/starter/contents.yaml",
			"resource: contents\nrecords:\n  - key: shaped-wrong\n    fields: {title: Shaped wrong, body: Body, kind: 7}\n",
			"contents", func(c composition) seed.Writer { return &contentSeeder{svc: c.contents} }},
		{"file visibility as a number", "seed/starter/files.yaml",
			"resource: files\nrecords:\n  - key: shaped-wrong\n    asset: assets/welcome.png\n    fields: {visibility: 0}\n",
			"files", func(c composition) seed.Writer { return fileSeeder{svc: c.files} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path, cfg := configure(t)
			install(t, path)
			c := compose(cfg)
			conn, err := db.Open(t.Context(), cfg.Database.URL)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = conn.Close() })
			files := fstest.MapFS{
				tc.file:                           {Data: []byte("apiVersion: platformkit.seed/v1\n" + tc.document)},
				"seed/starter/assets/welcome.png": {Data: asset},
			}
			writer := tc.writer(c)
			service, err := seed.New(seed.Deps{Files: files, Root: "seed", Clock: seedClock{},
				Writers: []seed.Writer{writer}, Authorize: seedGrants{auth: c.auth}})
			if err != nil {
				t.Fatal(err)
			}
			counts := func() (rows, keys int) {
				if err := dbtest.System(t.Context(), conn, func(ctx context.Context, system db.Tx[db.System]) error {
					tenant, err := c.tenants.ByHost(ctx, system, acmeHost)
					if err != nil {
						return err
					}
					return db.InTenant(ctx, system, tenant, func(_ context.Context, tx db.Tx[db.Tenant]) error {
						if err := tx.DB().Raw("SELECT count(*) FROM " + tc.table).Row().Scan(&rows); err != nil {
							return err
						}
						return tx.DB().Raw("SELECT count(*) FROM seed_keys WHERE key = 'shaped-wrong'").Row().Scan(&keys)
					})
				}); err != nil {
					t.Fatal(err)
				}
				return rows, keys
			}
			rowsBefore, _ := counts()
			var plan seed.Plan
			applyErr := dbtest.System(t.Context(), conn, func(ctx context.Context, system db.Tx[db.System]) error {
				tenant, err := c.tenants.ByHost(ctx, system, acmeHost)
				if err != nil {
					return err
				}
				return db.InTenant(ctx, system, tenant, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
					ctx, err = seedActor(ctx, c.users, tx, adminEmail)
					if err != nil {
						return err
					}
					plan, err = service.Apply(ctx, tx, seed.Selection{})
					return err
				})
			})
			if applyErr == nil {
				t.Errorf("seed accepted %s and committed: %s; want a refusal at %s", tc.name, plan, tc.file)
			} else if !strings.Contains(applyErr.Error(), tc.file+":") {
				t.Errorf("refusal = %v; want file and line", applyErr)
			}
			if len(plan.Items) != 0 {
				t.Errorf("refused record returned %d result rows; want none", len(plan.Items))
				if tc.table == "files" {
					var visibility string
					if err := dbtest.System(t.Context(), conn, func(ctx context.Context, system db.Tx[db.System]) error {
						tenant, err := c.tenants.ByHost(ctx, system, acmeHost)
						if err != nil {
							return err
						}
						return db.InTenant(ctx, system, tenant, func(_ context.Context, tx db.Tx[db.Tenant]) error {
							return tx.DB().Raw("SELECT visibility FROM files WHERE id = ?", plan.Items[0].RecordID).Row().Scan(&visibility)
						})
					}); err != nil {
						t.Fatal(err)
					}
					t.Errorf("the file the record declared visibility 0 for was stored visibility=%s", visibility)
				}
			}
			rowsAfter, keys := counts()
			if rowsAfter != rowsBefore || keys != 0 {
				t.Errorf("%s left %d new %s rows and %d seed keys; want none", tc.name, rowsAfter-rowsBefore, tc.table, keys)
			}
		})
	}
}
