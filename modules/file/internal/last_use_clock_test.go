package internal_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"image"
	"image/jpeg"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/file"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
	"github.com/septagon-oss/platformkit/modules/file/internal"
)

func TestEndingTheLastUseRecordsWhenTheGracePeriodStarts(t *testing.T) {
	_, conn := dbtest.Schema(t, file.Migrations)
	svc, manifest := file.New(file.Deps{Storage: internal.NewLocal(t.TempDir()), MaxBytes: 1 << 20,
		Tenants: sweepWalk{walking: []tenancy.Tenant{acme}}})
	ctx := tenancy.WithTenant(t.Context(), acme)
	var id uuid.UUID
	var picture bytes.Buffer
	if err := jpeg.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 20, 10)), nil); err != nil {
		t.Fatal(err)
	}
	use := contracts.Use{Module: "content", Entity: "content", Record: uuid.New(), Field: "body", Locale: "en"}
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		row, err := svc.Upload(ctx, held(tx), contracts.Upload{Name: "image.jpg", ContentType: "image/jpeg", Image: true, Body: bytes.NewReader(picture.Bytes()), Declared: int64(picture.Len())})
		if err != nil {
			return err
		}
		id = row.ID
		_, err = svc.SetUses(ctx, tx, use, []uuid.UUID{id})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	before := db.Now()
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		ended, err := svc.SetUses(ctx, tx, use, nil)
		if err == nil && (len(ended) != 1 || ended[0] != id) {
			t.Errorf("ended uses = %v, want %s", ended, id)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	after := db.Now()
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var since sql.NullTime
		// The specified last-use clock is read as JSON so an installation without
		// that column yields absence, rather than aborting the transaction.
		if err := tx.DB().Raw("SELECT (to_jsonb(files)->>'unused_since')::timestamptz FROM files WHERE id = ?", id).Row().Scan(&since); err != nil {
			return err
		}
		if !since.Valid || since.Time.Before(before) || since.Time.After(after) {
			t.Fatalf("last use ended between %s and %s, but its durable grace-period start is %v; a later sweep cannot measure 24h from this removal", before.Format(time.RFC3339Nano), after.Format(time.RFC3339Nano), since)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	runSweep := func() {
		t.Helper()
		for _, job := range manifest.Jobs {
			if job.Name == "file-unused" {
				if err := job.Run(t.Context(), conn); err != nil {
					t.Fatal(err)
				}
				return
			}
		}
		t.Fatal("the specified file-unused job is not mounted")
	}
	// As in the retention fixtures, elapsed time is seeded on the persisted
	// clock; the production sweep still reads its own ordinary clock.
	for _, age := range []time.Duration{24*time.Hour - time.Minute, 25 * time.Hour} {
		if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			return tx.DB().Exec("UPDATE files SET unused_since = ? WHERE id = ?", db.Now().Add(-age), id).Error
		}); err != nil {
			t.Fatal(err)
		}
		runSweep()
		if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			_, err := crud.Get[*contracts.File](tx, id)
			if age < 24*time.Hour {
				return err
			}
			if !errors.Is(err, crud.ErrNotFound) {
				t.Errorf("image after 25 hours without uses: %v, want released", err)
			}
			var charged int64
			if err := tx.DB().Model(&contracts.File{}).Select("COALESCE(SUM(size), 0)").Scan(&charged).Error; err != nil {
				return err
			}
			if charged != 0 {
				t.Errorf("released image still charges %d bytes", charged)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	runSweep()
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		for _, name := range []string{contracts.EventUploaded, contracts.EventReleased} {
			var count int64
			if err := tx.DB().Table(outbox).Where("name = ?", name).Count(&count).Error; err != nil {
				return err
			}
			if count != 1 {
				t.Errorf("%s outbox events = %d, want one", name, count)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
