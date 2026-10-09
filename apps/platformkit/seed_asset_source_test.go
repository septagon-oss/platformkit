package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime"
	"net/http"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/seed"
)

// pngFrame is a raster of w×h pixels — small, real, and told apart from every other
// raster in the fixture by the one fact about it the file module's image pass does
// not move: its frame. Decision 0069 §4 re-encodes what arrives, so byte-for-byte
// equality with the bytes beside a record is no longer what a stored upload promises;
// which pixels are behind the row still is.
func pngFrame(t *testing.T, w, h int) []byte {
	t.Helper()
	frame := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			frame.Set(x, y, color.RGBA{R: uint8(x * 17), G: uint8(y * 23), B: 91, A: 255})
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, frame); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

// TestSeedUploadsTheAssetItsDocumentNames asks which bytes a seed run put into
// storage. The answer it checks for is the record's own path: the fixture carries
// two rasters of different frames, the record names one, and the frame the store
// serves is the named one's. The bytes are read through the file module's own Open,
// and the row's digest is the digest of exactly those bytes — which is the pair the
// module writes after its image pass (decision 0069 §4: the row and the object carry
// what the frame re-encoded to, not what was sent), and the reason the claim is
// framed by frame and digest rather than by equality with the file on disk.
func TestSeedUploadsTheAssetItsDocumentNames(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	const (
		width, height = 9, 4
	)
	files := fstest.MapFS{
		"seed/starter/files.yaml": {Data: []byte(`apiVersion: platformkit.seed/v1
resource: files
records:
  - key: starter-image
    asset: assets/welcome.png
`)},
		"seed/starter/assets/welcome.png": {Data: pngFrame(t, width, height)},
		// A second asset in the same tree, so "it uploaded something" is not an
		// answer: a run that read the wrong path uploads a 3×3 frame.
		"seed/starter/assets/other.png": {Data: pngFrame(t, 3, 3)},
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
			frame, _, err := image.DecodeConfig(bytes.NewReader(stored))
			if err != nil {
				return err
			}
			if frame.Width != width || frame.Height != height {
				t.Errorf("the stored frame is %dx%d; the record named a %dx%d asset, and the other asset in the same tree is 3x3",
					frame.Width, frame.Height, width, height)
			}
			if row.Width != width || row.Height != height {
				t.Errorf("the row says %dx%d, which is not the frame the store serves (%dx%d)",
					row.Width, row.Height, frame.Width, frame.Height)
			}
			digest := sha256.Sum256(stored)
			if row.SHA256 != hex.EncodeToString(digest[:]) {
				t.Errorf("row digest %s is not the digest of the bytes a reader is served", row.SHA256)
			}
			if sniffed, _, _ := mime.ParseMediaType(http.DetectContentType(stored)); sniffed != row.ContentType {
				t.Errorf("the row's media type %q is not the type of the bytes a reader is served (%q)",
					row.ContentType, sniffed)
			}
			return nil
		})
	}); err != nil {
		t.Fatal(err)
	}
}
