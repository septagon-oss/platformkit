package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/seed"
)

func TestSeedUploadsTheAssetItsDocumentNames(t *testing.T) {
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
	// A PNG may carry trailing bytes; its signature and decoded image stay valid.
	asset = append(asset, []byte("declared asset bytes")...)
	digest := sha256.Sum256(asset)
	files := fstest.MapFS{
		"seed/starter/files.yaml": {Data: []byte(`apiVersion: platformkit.seed/v1
resource: files
records:
  - key: starter-image
    asset: assets/welcome.png
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
			if len(plan.Items) != 1 {
				t.Fatalf("seed result = %s; want one file", plan)
			}
			row, body, err := c.files.Open(ctx, tx, plan.Items[0].RecordID, false)
			if err != nil {
				return err
			}
			defer body.Close()
			stored, err := io.ReadAll(body)
			if err != nil {
				return err
			}
			if !bytes.Equal(stored, asset) || row.SHA256 != hex.EncodeToString(digest[:]) {
				t.Errorf("uploaded %d bytes with digest %s; document names %d bytes with digest %x",
					len(stored), row.SHA256, len(asset), digest)
			}
			return nil
		})
	}); err != nil {
		t.Fatal(err)
	}
}
