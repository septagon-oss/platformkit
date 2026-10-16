package internal_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/file"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
	"github.com/septagon-oss/platformkit/modules/file/internal"
)

// TestTheOrphanSweepKeepsTheWidthsOfAStoredImage is the two jobs of this module
// that touch the store, run one after the other.
//
// A width is an object with no files row: its name lives in file_variants. The
// reconciliation sweep removes every object older than an hour that no row
// names, so it has to count a ready width's row as a name, or the night after an
// image is encoded every one of its widths is gone while its row still says
// ready — the srcset keeps offering it, the door answers it with an outage, and
// the quota keeps charging for bytes that are not there.
func TestTheOrphanSweepKeepsTheWidthsOfAStoredImage(t *testing.T) {
	_, conn := dbtest.Schema(t, file.Migrations)
	dir := t.TempDir()
	store := internal.NewLocal(dir)
	// A photograph is larger than the conformance suite's kilobyte.
	svc := internal.NewService(store, 16<<20, 0, 0)
	ctx := tenancy.WithTenant(t.Context(), acme)

	frame := image.NewRGBA(image.Rect(0, 0, 1000, 500))
	for x := range 1000 {
		for y := range 500 {
			frame.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 90, A: 255})
		}
	}
	var picture bytes.Buffer
	if err := jpeg.Encode(&picture, frame, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("encode the picture: %v", err)
	}

	var id uuid.UUID
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		f, err := svc.Upload(ctx, held(tx), contracts.Upload{Name: "wide.jpg", ContentType: "image/jpeg",
			Declared: int64(picture.Len()), Body: bytes.NewReader(picture.Bytes())})
		if err != nil {
			return err
		}
		id = f.ID
		return nil
	}); err != nil {
		t.Fatalf("upload a 1000 px picture: %v", err)
	}

	encode := internal.NewVariants(svc, store, sweepWalk{walking: []tenancy.Tenant{acme}}, time.Second).Jobs()
	if len(encode) != 1 {
		t.Fatalf("the module schedules %d encode jobs, want one", len(encode))
	}
	if err := encode[0].Run(t.Context(), conn); err != nil {
		t.Fatalf("the encode pass: %v", err)
	}
	opens := func() error {
		return db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			_, body, err := svc.OpenWidth(ctx, tx, id, false, 480)
			if err != nil {
				return err
			}
			defer body.Close()
			_, err = io.Copy(io.Discard, body)
			return err
		})
	}
	if err := opens(); err != nil {
		t.Fatalf("the 480 px width of a 1000 px picture after one encode pass: %v", err)
	}

	// A night later: everything the store holds is older than the sweep's hour.
	old := time.Now().Add(-2 * time.Hour)
	if err := filepath.WalkDir(dir, func(at string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		return os.Chtimes(at, old, old)
	}); err != nil {
		t.Fatalf("age what the store holds: %v", err)
	}
	sweep := internal.NewReconcile(store, time.Second)
	sweep.Use(dbtest.SystemToken())
	if err := sweep.Jobs()[0].Run(t.Context(), conn); err != nil {
		t.Fatalf("the reconciliation sweep: %v", err)
	}

	// A 1000 px frame plans 480 and 960: the frame and two widths, all named by a row.
	want := 1 + len(contracts.VariantWidths(1000))
	if got := len(keysUnder(t, dir)); got != want {
		t.Errorf("the store holds %d objects after the sweep, want %d: the frame and its ready widths", got, want)
	}
	if err := opens(); err != nil {
		t.Errorf("the 480 px width the row still calls ready, after the orphan sweep: %v", err)
	}
}
