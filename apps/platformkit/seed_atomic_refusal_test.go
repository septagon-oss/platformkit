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
	contentcontracts "github.com/septagon-oss/platformkit/modules/content/contracts"
)

func TestSeedOwnerRefusalRollsBackEarlierRowEventAndKey(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	service, err := seed.New(seed.Deps{
		Files: fstest.MapFS{"seed/starter/contents.yaml": {Data: []byte(`apiVersion: platformkit.seed/v1
resource: contents
records:
  - key: valid-before-refusal
    fields: {kind: page, title: Valid, body: First write}
  - key: untitled-after-valid
    fields: {kind: page, title: " ", body: Refused write}
`)}},
		Root: "seed", Clock: seedClock{},
		Writers: []seed.Writer{&contentSeeder{svc: c.contents}}, Authorize: seedGrants{auth: c.auth},
	})
	if err != nil {
		t.Fatal(err)
	}

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
			_, err = service.Apply(ctx, tx, seed.Selection{})
			return err
		})
	})
	if applyErr == nil || !strings.Contains(applyErr.Error(), "a page needs a title") ||
		!strings.Contains(applyErr.Error(), "seed/starter/contents.yaml:") {
		t.Fatalf("Apply error = %v; want the owner refusal at the second record's line", applyErr)
	}

	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, system db.Tx[db.System]) error {
		tenant, err := c.tenants.ByHost(ctx, system, acmeHost)
		if err != nil {
			return err
		}
		return db.InTenant(ctx, system, tenant, func(_ context.Context, tx db.Tx[db.Tenant]) error {
			_, rows, err := crud.List[*contentcontracts.Content](tx, crud.Query{Filter: map[string]any{"slug": "valid-before-refusal"}, Limit: 2})
			if err != nil {
				return err
			}
			var keys, events int
			if err := tx.DB().Raw(`SELECT count(*) FROM seed_keys WHERE key = 'valid-before-refusal'`).Row().Scan(&keys); err != nil {
				return err
			}
			if err := tx.DB().Raw(`SELECT count(*) FROM platformkit_outbox WHERE name = 'content.content.created' AND payload->>'slug' = 'valid-before-refusal'`).Row().Scan(&events); err != nil {
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
}
