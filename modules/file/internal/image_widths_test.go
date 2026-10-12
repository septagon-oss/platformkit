package internal_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/richtext"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/file"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
	"github.com/septagon-oss/platformkit/modules/file/internal"
)

func TestImageWidthsAreEncodedAdvertisedAndCharged(t *testing.T) {
	for _, width := range []int{700, 2400} {
		t.Run(strconv.Itoa(width), func(t *testing.T) {
			_, conn := dbtest.Schema(t, file.Migrations)
			ctx := tenancy.WithTenant(t.Context(), acme)
			store := internal.NewLocal(t.TempDir())
			svc := internal.NewService(store, 16<<20, 0, 0)
			var picture bytes.Buffer
			if err := jpeg.Encode(&picture, image.NewRGBA(image.Rect(0, 0, width, width/2)), nil); err != nil {
				t.Fatal(err)
			}
			var parent *contracts.File
			if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				var err error
				parent, err = svc.Upload(ctx, held(tx), contracts.Upload{Name: "wide.jpg", ContentType: "image/jpeg", Body: bytes.NewReader(picture.Bytes()), Declared: int64(picture.Len())})
				return err
			}); err != nil {
				t.Fatal(err)
			}
			job := internal.NewVariants(svc, store, sweepWalk{walking: []tenancy.Tenant{acme}}, time.Second).Jobs()[0]
			for range 2 {
				if err := job.Run(ctx, conn); err != nil {
					t.Fatal(err)
				}
			}
			charged := parent.Size
			if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				rows, err := svc.Variants(ctx, tx, parent.ID)
				if err != nil {
					return err
				}
				want := []int{480}
				if width == 2400 {
					want = []int{480, 960, 1440, 1920}
				}
				if len(rows) != len(want) {
					t.Fatalf("built %d widths, want %v", len(rows), want)
				}
				for i, row := range rows {
					_, body, err := svc.OpenWidth(ctx, tx, parent.ID, false, row.Width)
					if err != nil {
						return err
					}
					frame, _, err := image.DecodeConfig(body)
					body.Close()
					if err != nil || frame.Width != want[i] || frame.Height != want[i]/2 {
						t.Errorf("encoded width %d = %+v, %v; want %dx%d", row.Width, frame, err, want[i], want[i]/2)
					}
					charged += row.Size
				}
				port := file.RichTextFiles{Opener: svc, Variants: svc}
				doc, err := richtext.Parse("![Landscape](pk-file:" + parent.ID.String() + ")")
				if err != nil {
					return err
				}
				html, err := richtext.Render(ctx, tx, doc, port, richtext.Workspace)
				if err != nil {
					return err
				}
				for _, w := range want {
					candidate := "?w=" + strconv.Itoa(w) + " " + strconv.Itoa(w) + "w"
					if !strings.Contains(html, candidate) {
						t.Errorf("render omitted %q: %s", candidate, html)
					}
				}
				if !strings.Contains(html, `sizes="`+richtext.ProseSizes+`"`) {
					t.Errorf("render omitted sizes: %s", html)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			limited := internal.NewService(store, 16<<20, charged, 0)
			err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				_, err := limited.Upload(ctx, held(tx), contracts.Upload{Name: "extra.txt", ContentType: "text/plain", Body: strings.NewReader("x"), Declared: 1})
				return err
			})
			if !errors.Is(err, contracts.ErrQuota) {
				t.Errorf("quota of original plus widths accepted another byte: %v", err)
			}
		})
	}
}
