package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"io"
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

// The record's `asset` is a declared value the run reads, and a rerun answers it by
// writing nothing: an upload writes its bytes once, the owner runs its own image pass
// over what arrived (decision 0069 §4 — the row and the object carry the frame it
// re-encoded to), and no seed run puts others behind a row that already has some.
//
// So a later deploy whose asset moved converges rather than refuses. The comparison
// it declines to make is the one the owner cannot win: the digest on the row belongs
// to the stored frame, not to the bytes beside the record, and refusing a run that
// found every managed fact already settled is refusing the write that is not there to
// take. What this case holds is the half that is this run's to hold — that the later
// deploy's bytes reached neither the object store nor the row, that no second upload
// appeared, and that the record the run read is the record the tenant already had.
func TestSeedWritesNoBytesOverAnExistingUpload(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	document := `apiVersion: platformkit.seed/v1
resource: files
records:
  - key: starter-image
    asset: assets/welcome.png
`
	// Two rasters of different frames: the second deploy's asset is not a re-encode
	// of the first, so a run that did write bytes would be seen in the frame.
	first, second := pngFrame(t, 8, 5), pngFrame(t, 2, 7)
	fixture := func(body []byte) fs.FS {
		return fstest.MapFS{
			"seed/starter/files.yaml":         {Data: []byte(document)},
			"seed/starter/assets/welcome.png": {Data: body},
		}
	}
	apply := func(t *testing.T, files fs.FS) (seed.Plan, error) {
		t.Helper()
		service, err := seed.New(seed.Deps{Files: files, Root: "seed", Clock: seedClock{},
			Writers: []seed.Writer{fileSeeder{svc: c.files}}, Authorize: seedGrants{auth: c.auth}})
		if err != nil {
			return seed.Plan{}, err
		}
		var plan seed.Plan
		err = dbtest.System(t.Context(), conn, func(ctx context.Context, system db.Tx[db.System]) error {
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
		return plan, err
	}
	created, err := apply(t, fixture(first))
	if err != nil {
		t.Fatalf("first seed run: %v", err)
	}
	if len(created.Items) != 1 || created.Items[0].Action != seed.Create {
		t.Fatalf("first seed run = %s; want one created file", created)
	}
	rerun, err := apply(t, fixture(second))
	if err != nil {
		t.Fatalf("rerun whose asset moved: %v (plan: %s)", err, rerun)
	}
	if len(rerun.Items) != 1 || rerun.Items[0].Action != seed.Unchanged {
		t.Errorf("rerun = %s; want the one record unchanged", rerun)
	}
	if !strings.Contains(rerun.String(), "UNCHANGED files/starter-image") {
		t.Errorf("rerun plan does not name the record it left alone:\n%s", rerun)
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
				t.Errorf("the rerun left %d file rows; want the one upload the first run made", len(rows))
				return nil
			}
			row := rows[0]
			if row.ID != created.Items[0].RecordID {
				t.Errorf("the rerun answered key starter-image with %s, not the row that already had it (%s)",
					row.ID, created.Items[0].RecordID)
			}
			storedFile, reader, err := c.files.Open(ctx, tx, row.ID, false)
			if err != nil {
				return err
			}
			defer reader.Close()
			if storedFile.ID != row.ID {
				t.Errorf("Open answered %s for the row this case opened (%s)", storedFile.ID, row.ID)
			}
			stored, err := io.ReadAll(reader)
			if err != nil {
				return err
			}
			frame, _, err := image.DecodeConfig(bytes.NewReader(stored))
			if err != nil {
				return err
			}
			if frame.Width != 8 || frame.Height != 5 {
				t.Errorf("the bytes a reader is served are a %dx%d frame; the rerun must leave the first deploy's 8x5 alone",
					frame.Width, frame.Height)
			}
			digest := sha256.Sum256(stored)
			if row.SHA256 != hex.EncodeToString(digest[:]) {
				t.Errorf("row digest %s is not the digest of the stored bytes", row.SHA256)
			}
			return nil
		})
	}); err != nil {
		t.Fatal(err)
	}
}
