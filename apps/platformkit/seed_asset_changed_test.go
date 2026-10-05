package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/seed"
	filecontracts "github.com/septagon-oss/platformkit/modules/file/contracts"
)

// The record's `asset` is a declared value the run reads, so a rerun answers it.
// An upload writes its bytes once and the seed puts none over them, which is the
// only honest answer for a row that already has some — but the shape the run must
// never take is a rerun that reads new bytes beside the record, compares nothing,
// and prints UNCHANGED while the reader keeps being served the old ones. So the
// compared value is the digest the file module already stores, and a changed asset
// refuses at the record whose bytes moved.
func TestSeedRefusesAnAssetThatChangedSinceItsUpload(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	png, err := fs.ReadFile(seedFiles, "seed/demo/assets/welcome.png")
	if err != nil {
		t.Fatal(err)
	}
	document := []byte(`apiVersion: platformkit.seed/v1
resource: files
records:
  - key: starter-image
    asset: assets/welcome.png
`)
	stored := func(body []byte) string {
		digest := sha256.Sum256(body)
		return hex.EncodeToString(digest[:])
	}
	first, second := append(append([]byte{}, png...), []byte("the bytes at the first deploy")...),
		append(append([]byte{}, png...), []byte("the bytes a later deploy puts there")...)
	fixture := func(body []byte) fs.FS {
		return fstest.MapFS{
			"seed/starter/files.yaml":         {Data: document},
			"seed/starter/assets/welcome.png": {Data: body},
		}
	}
	apply := func(t *testing.T, files fs.FS) error {
		t.Helper()
		service, err := seed.New(seed.Deps{Files: files, Root: "seed", Clock: seedClock{},
			Writers: []seed.Writer{fileSeeder{svc: c.files}}, Authorize: seedGrants{auth: c.auth}})
		if err != nil {
			return err
		}
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
				_, err = service.Apply(ctx, tx, seed.Selection{})
				return err
			})
		})
	}
	if err := apply(t, fixture(first)); err != nil {
		t.Fatalf("first seed run: %v", err)
	}
	err = apply(t, fixture(second))
	if err == nil {
		t.Fatal("a rerun whose asset changed reported success")
	}
	if !strings.Contains(err.Error(), "asset") {
		t.Errorf("refusal %q does not name the asset it could not apply", err)
	}
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, system db.Tx[db.System]) error {
		tenant, err := c.tenants.ByHost(ctx, system, acmeHost)
		if err != nil {
			return err
		}
		return db.InTenant(ctx, system, tenant, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			rows, _, err := crud.List[*filecontracts.File](tx, crud.Query{Limit: 10})
			if err != nil {
				return err
			}
			if len(rows) != 1 {
				t.Errorf("the refused run left %d file rows; want the one upload it made before", len(rows))
				return nil
			}
			if rows[0].SHA256 != stored(first) {
				t.Errorf("stored digest %s is not the first deploy's %s", rows[0].SHA256, stored(first))
			}
			return nil
		})
	}); err != nil {
		t.Fatal(err)
	}
}
