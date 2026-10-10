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
	filecontracts "github.com/septagon-oss/platformkit/modules/file/contracts"
)

func TestSeedRefusesAFileVisibilityChangeWithoutRowsKeysOrEvents(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	inTenant := func(fn func(context.Context, db.Tx[db.Tenant]) error) error {
		return dbtest.System(t.Context(), conn, func(ctx context.Context, system db.Tx[db.System]) error {
			tenant, err := c.tenants.ByHost(ctx, system, acmeHost)
			if err != nil {
				return err
			}
			return db.InTenant(ctx, system, tenant, fn)
		})
	}
	asset := pngFrame(t, 8, 5)
	apply := func(visibility string) (seed.Plan, error) {
		service, err := seed.New(seed.Deps{
			Files: fstest.MapFS{
				"seed/starter/files.yaml": {Data: []byte(`apiVersion: platformkit.seed/v1
resource: files
records:
  - key: visible-image
    asset: assets/welcome.png
    fields: {visibility: ` + visibility + `}
`)},
				"seed/starter/assets/welcome.png": {Data: asset},
			},
			Root: "seed", Clock: seedClock{},
			Writers: []seed.Writer{fileSeeder{svc: c.files}}, Authorize: seedGrants{auth: c.auth},
		})
		if err != nil {
			return seed.Plan{}, err
		}
		var plan seed.Plan
		err = inTenant(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			ctx, err := seedActor(ctx, c.users, tx, adminEmail)
			if err != nil {
				return err
			}
			plan, err = service.Apply(ctx, tx, seed.Selection{})
			return err
		})
		return plan, err
	}
	created, err := apply(filecontracts.VisibilityPublic)
	if err != nil || len(created.Items) != 1 || created.Items[0].Action != seed.Create {
		t.Fatalf("initial upload = %s, %v; want one created file", created, err)
	}
	id := created.Items[0].RecordID
	if id == uuid.Nil {
		t.Fatal("created file has no identity")
	}
	refused, err := apply(filecontracts.VisibilityPrivate)
	if err == nil || !strings.Contains(err.Error(), "seed/starter/files.yaml:") {
		t.Fatalf("changed visibility = %s, %v; want a refusal at its seed source", refused, err)
	}
	if len(refused.Items) != 0 {
		t.Errorf("refused run returned %d result items", len(refused.Items))
	}
	if err := inTenant(func(_ context.Context, tx db.Tx[db.Tenant]) error {
		row, err := crud.Get[*filecontracts.File](tx, id)
		if err != nil {
			return err
		}
		var rows, keys, events int
		if err := tx.DB().Raw(`SELECT count(*) FROM files WHERE name = 'welcome.png'`).Row().Scan(&rows); err != nil {
			return err
		}
		if err := tx.DB().Raw(`SELECT count(*) FROM seed_keys WHERE key = 'visible-image' AND record_id = ?`, id).Row().Scan(&keys); err != nil {
			return err
		}
		if err := tx.DB().Raw(`SELECT count(*) FROM platformkit_outbox WHERE name LIKE 'file.%' AND payload->>'fileId' = ?`, id.String()).Row().Scan(&events); err != nil {
			return err
		}
		if row.Visibility != filecontracts.VisibilityPublic || rows != 1 || keys != 1 || events != 1 {
			t.Errorf("refused run left visibility=%s, rows=%d, keys=%d, events=%d; want public, 1, 1, 1", row.Visibility, rows, keys, events)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
