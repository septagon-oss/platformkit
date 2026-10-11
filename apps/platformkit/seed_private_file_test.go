package main

import (
	"context"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/seed"
	filecontracts "github.com/septagon-oss/platformkit/modules/file/contracts"
)

func TestSeededPrivateFileCannotBeReadAnonymously(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	asset, err := fs.ReadFile(seedFiles, "seed/demo/assets/welcome.png")
	if err != nil {
		t.Fatal(err)
	}
	files := fstest.MapFS{
		"seed/starter/files.yaml": {Data: []byte(`apiVersion: platformkit.seed/v1
resource: files
records:
  - key: private-image
    asset: assets/welcome.png
    fields: {visibility: private}
`)},
		"seed/starter/assets/welcome.png": {Data: asset},
	}
	service, err := seed.New(seed.Deps{Files: files, Root: "seed", Clock: seedClock{},
		Writers: []seed.Writer{fileSeeder{svc: c.files}}, Authorize: seedGrants{auth: c.auth}})
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
			plan, err := service.Apply(ctx, tx, seed.Selection{})
			if err != nil {
				return err
			}
			if len(plan.Items) != 1 || plan.Items[0].RecordID == uuid.Nil {
				t.Fatalf("seed result = %s; want one uploaded record", plan)
			}
			id := plan.Items[0].RecordID
			row, err := crud.Get[*filecontracts.File](tx, id)
			if err != nil {
				return err
			}
			if row.Visibility != filecontracts.VisibilityPrivate {
				t.Errorf("file declared visibility=private was stored visibility=%s", row.Visibility)
			}
			_, body, anonymousErr := c.files.Open(ctx, tx, id, true)
			if body != nil {
				_ = body.Close()
			}
			if anonymousErr == nil {
				t.Error("anonymous read succeeded for the file declared private")
			}
			return nil
		})
	}); err != nil {
		t.Fatal(err)
	}
}
