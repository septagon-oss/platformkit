package internal_test

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/file"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
	"github.com/septagon-oss/platformkit/modules/file/internal"
)

// TestARecordThatRefusesTheRowAfterTheFrameIsStoredRemovesTheFrameItStored asks
// about the last refusal an upload can meet: the name is longer than the record's
// own ceiling, so it is refused where a name is checked — kit/crud running
// File.Validate on the write, inside the transaction, after the bytes arrived,
// after the frame was re-encoded and stored and after the quota was charged. From
// the frame's storage onward the only object this upload owns is the frame the row
// would have named, so the refusal owes that removal: what is left is a byte no row
// names, that no door serves, that the local store keeps until its daily sweep and
// that the S3 adapter leaves forever because it runs no sweep at all.
//
// The refusal's own words are what show the case reached that point — nothing
// before the image pass and the charge reads a name — and the store's silence is
// what the module promises beyond it.
func TestARecordThatRefusesTheRowAfterTheFrameIsStoredRemovesTheFrameItStored(t *testing.T) {
	_, conn := dbtest.Schema(t, file.Migrations)
	dir := t.TempDir()
	svc := internal.NewService(internal.NewLocal(dir), 16<<20, 0, 0)

	var picture bytes.Buffer
	if err := jpeg.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 40, 20)), nil); err != nil {
		t.Fatal(err)
	}
	// Characters, not bytes: the record counts runes, so a name that is one
	// character past the ceiling is refused for its length and for nothing else.
	name := strings.Repeat("é", contracts.MaxName+1)

	ctx := tenancy.WithTenant(t.Context(), acme)
	err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := svc.Upload(ctx, held(tx), contracts.Upload{
			Name: name, ContentType: "image/jpeg",
			Body: bytes.NewReader(picture.Bytes()), Declared: int64(picture.Len()),
		})
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "longer than") {
		t.Fatalf("an upload of a %d-character name = %v, want the record's own refusal of the name",
			contracts.MaxName+1, err)
	}
	if got := keysUnder(t, dir); len(got) != 0 {
		t.Errorf("the refused upload left %v in the store, want no byte of either object it wrote", got)
	}
}
