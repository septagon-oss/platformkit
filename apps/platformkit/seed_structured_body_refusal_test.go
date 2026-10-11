package main

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/seed"
)

func TestSeedRefusesStructuredBodyWithoutRowsKeysEventsOrResults(t *testing.T) {
	for name, value := range map[string]string{"list": "[private, text]", "mapping": "{private: text}", "boolean": "true"} {
		t.Run(name, func(t *testing.T) {
			path, cfg := configure(t)
			install(t, path)
			c := compose(cfg)
			conn, err := db.Open(t.Context(), cfg.Database.URL)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = conn.Close() })

			service, err := seed.New(seed.Deps{
				Files: fstest.MapFS{"seed/starter/contents.yaml": {Data: []byte(strings.ReplaceAll(`apiVersion: platformkit.seed/v1
resource: contents
records:
  - key: valid-before-structured-body
    fields: {kind: page, title: Valid, body: First write}
  - key: structured-body
    fields: {kind: page, title: Structured body, body: BODY_VALUE}
`, "BODY_VALUE", value))}},
				Root: "seed", Clock: seedClock{},
				Writers: []seed.Writer{&contentSeeder{svc: c.contents}}, Authorize: seedGrants{auth: c.auth},
			})
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
			if applyErr == nil || !strings.Contains(applyErr.Error(), "body") ||
				!strings.Contains(applyErr.Error(), "seed/starter/contents.yaml:") {
				t.Fatalf("Apply error = %v; want a body refusal at the second record's line", applyErr)
			}

			if len(plan.Items) != 0 {
				t.Errorf("refused run returned %d result items; want none", len(plan.Items))
			}

			if err := dbtest.System(t.Context(), conn, func(ctx context.Context, system db.Tx[db.System]) error {
				tenant, err := c.tenants.ByHost(ctx, system, acmeHost)
				if err != nil {
					return err
				}
				return db.InTenant(ctx, system, tenant, func(_ context.Context, tx db.Tx[db.Tenant]) error {
					var rows, keys, events int
					if err := tx.DB().Raw(`SELECT count(*) FROM contents WHERE slug IN ('valid-before-structured-body', 'structured-body')`).Row().Scan(&rows); err != nil {
						return err
					}
					if err := tx.DB().Raw(`SELECT count(*) FROM seed_keys WHERE key IN ('valid-before-structured-body', 'structured-body')`).Row().Scan(&keys); err != nil {
						return err
					}
					if err := tx.DB().Raw(`SELECT count(*) FROM platformkit_outbox WHERE name = 'content.content.created' AND payload->>'slug' IN ('valid-before-structured-body', 'structured-body')`).Row().Scan(&events); err != nil {
						return err
					}
					if rows != 0 || keys != 0 || events != 0 {
						t.Errorf("refused run committed %d earlier rows, %d seed keys, %d outbox events", rows, keys, events)
					}
					return nil
				})
			}); err != nil {
				t.Fatal(err)
			}

		})
	}
}
