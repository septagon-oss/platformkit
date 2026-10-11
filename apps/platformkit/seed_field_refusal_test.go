package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/seed"
)

func TestSeedRefusesFieldsItCannotApplyThroughTheOwner(t *testing.T) {
	for _, tc := range []struct{ name, field string }{
		{"unknown field", "notAField: true"},
		{"read-only field", "resolvedAt: '2026-10-05T00:00:00Z'"},
		{"invalid lifecycle enum", "status: impossible"},
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
			const file = "seed/starter/tasks.yaml"
			files := fstest.MapFS{file: {Data: fmt.Appendf(nil, `apiVersion: platformkit.seed/v1
resource: tasks
records:
  - key: refused-field
    fields: {title: Refused field, %s}
`, tc.field)}}
			service, err := seed.New(seed.Deps{Files: files, Root: "seed", Clock: seedClock{},
				Writers: []seed.Writer{taskSeeder{svc: c.tasks}}, Authorize: seedGrants{auth: c.auth}})
			if err != nil {
				t.Fatal(err)
			}
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
				t.Errorf("seed accepted %s and committed: %s; want a sourced refusal", tc.field, plan)
			} else if !strings.Contains(applyErr.Error(), file+":") {
				t.Errorf("refusal = %v; want file and line", applyErr)
			}
			if len(plan.Items) != 0 {
				t.Errorf("refused field returned %d result rows; want none", len(plan.Items))
			}
			if err := dbtest.System(t.Context(), conn, func(ctx context.Context, system db.Tx[db.System]) error {
				tenant, err := c.tenants.ByHost(ctx, system, acmeHost)
				if err != nil {
					return err
				}
				return db.InTenant(ctx, system, tenant, func(_ context.Context, tx db.Tx[db.Tenant]) error {
					var rows, keys, events int
					if err := tx.DB().Raw("SELECT count(*) FROM tasks WHERE title = 'Refused field'").Row().Scan(&rows); err != nil {
						return err
					}
					if err := tx.DB().Raw("SELECT count(*) FROM seed_keys WHERE key = 'refused-field'").Row().Scan(&keys); err != nil {
						return err
					}
					if err := tx.DB().Raw("SELECT count(*) FROM platformkit_outbox WHERE payload->>'title' = 'Refused field'").Row().Scan(&events); err != nil {
						return err
					}
					if rows != 0 || keys != 0 || events != 0 {
						t.Errorf("unapplied field left %d rows, %d keys, %d events; want zero", rows, keys, events)
					}
					return nil
				})
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
