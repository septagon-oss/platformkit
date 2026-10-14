package internal_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
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

// TestAWidthIsChargedToItsTenantAndServedToNoOther is the encode pass read
// against the quota and against row-level security.
//
// A tenant whose quota holds the frame and its 480 px width but not its 960 px
// one gets the 480 built and charged, and the 960 recorded failed with the
// reason "quota" — whichever of the two the pass took first, because the 960 is
// always the larger. Another tenant asking for the built width of this tenant's
// picture learns nothing: not found, at the door and at the srcset's read.
func TestAWidthIsChargedToItsTenantAndServedToNoOther(t *testing.T) {
	_, conn := dbtest.Schema(t, file.Migrations)
	globex := tenancy.Tenant{ID: uuid.New(), Slug: "globex", Name: "Globex"}

	picture := func() []byte {
		frame := image.NewRGBA(image.Rect(0, 0, 1000, 500))
		for x := range 1000 {
			for y := range 500 {
				frame.Set(x, y, color.RGBA{R: uint8(x * y), G: uint8(y), B: uint8(x), A: 255})
			}
		}
		var out bytes.Buffer
		if err := jpeg.Encode(&out, frame, &jpeg.Options{Quality: 90}); err != nil {
			t.Fatalf("encode the picture: %v", err)
		}
		return out.Bytes()
	}()
	// encodeIn uploads the picture as one tenant and runs one encode pass over
	// that tenant, answering the file and the size of every row it left.
	encodeIn := func(tenant tenancy.Tenant, quota int64) (uuid.UUID, int64, map[int]contracts.Variant) {
		t.Helper()
		store := internal.NewLocal(t.TempDir())
		svc := internal.NewService(store, 16<<20, quota, 0)
		ctx := tenancy.WithTenant(t.Context(), tenant)
		var f *contracts.File
		if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			var err error
			f, err = svc.Upload(ctx, held(tx), contracts.Upload{Name: "wide.jpg", ContentType: "image/jpeg",
				Declared: int64(len(picture)), Body: bytes.NewReader(picture)})
			return err
		}); err != nil {
			t.Fatalf("upload as %s: %v", tenant.Slug, err)
		}
		jobs := internal.NewVariants(svc, store, sweepWalk{walking: []tenancy.Tenant{tenant}}, time.Second).Jobs()
		if err := jobs[0].Run(t.Context(), conn); err != nil {
			t.Fatalf("encode pass over %s: %v", tenant.Slug, err)
		}
		rows := map[int]contracts.Variant{}
		if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			var all []contracts.Variant
			if err := tx.DB().Where("file_id = ?", f.ID).Find(&all).Error; err != nil {
				return err
			}
			for _, r := range all {
				rows[r.Width] = r
			}
			return nil
		}); err != nil {
			t.Fatalf("read %s's widths: %v", tenant.Slug, err)
		}
		return f.ID, f.Size, rows
	}

	// Measured with room for everything, in a tenant of its own: the encode is
	// deterministic, so these are the sizes the tight tenant's pass will meet.
	_, frameSize, roomy := encodeIn(globex, 0)
	if roomy[480].State != contracts.VariantReady || roomy[960].State != contracts.VariantReady {
		t.Fatalf("with no quota both widths are built, got %+v", roomy)
	}

	id, _, tight := encodeIn(acme, frameSize+roomy[480].Size+1)
	if got := tight[480]; got.State != contracts.VariantReady || got.Size != roomy[480].Size {
		t.Errorf("the width that fits: state %q size %d, want ready and %d bytes", got.State, got.Size, roomy[480].Size)
	}
	if got := tight[960]; got.State != contracts.VariantFailed || got.Reason != contracts.VariantFailedQuota {
		t.Errorf("the width that does not fit: state %q reason %q, want failed for %q", got.State, got.Reason, contracts.VariantFailedQuota)
	}

	// Globex asks for acme's built width.
	store := internal.NewLocal(t.TempDir())
	svc := internal.NewService(store, 16<<20, 0, 0)
	err := db.Run(tenancy.WithTenant(t.Context(), globex), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if _, _, err := svc.OpenWidth(ctx, tx, id, false, 480); !errors.Is(err, crud.ErrNotFound) {
			t.Errorf("another tenant opened this tenant's 480 px width: %v", err)
		}
		if rows, err := svc.Variants(ctx, tx, id); !errors.Is(err, crud.ErrNotFound) {
			t.Errorf("another tenant read this tenant's widths: %v, %v", rows, err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("globex's transaction: %v", err)
	}
}
