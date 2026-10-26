package internal_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/file"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
	"github.com/septagon-oss/platformkit/modules/file/internal"
)

// TestAnUploadTheQuotaRefusesAfterTheImagePassRemovesEveryByteItWrote asks the
// refusal that arrives last — the quota, weighed inside the transaction, after
// every byte is on disk — to leave nothing behind.
//
// An image is two objects while the upload runs: the bytes that arrived, and the
// frame the image pass re-encoded them into. The pass stores the second and the
// upload removes the first, so from that moment the one object a refusal still
// has to remove is the re-encoded frame, which no row has named yet and no
// reader will ever fetch. A refused upload writes nothing, and "nothing" counts
// the bytes as well as the rows: the local store keeps what is left until its
// daily sweep, and the S3 adapter states in its own comment that it runs no
// sweep at all.
func TestAnUploadTheQuotaRefusesAfterTheImagePassRemovesEveryByteItWrote(t *testing.T) {
	_, conn := dbtest.Schema(t, file.Migrations)
	dir := t.TempDir()
	store := internal.NewLocal(dir)
	// One byte of quota: the frame re-encodes to more, so the refusal is the
	// quota's and it lands after the image pass has run.
	svc := internal.NewService(store, 16<<20, 1, 0)

	var picture bytes.Buffer
	if err := jpeg.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 40, 20)), nil); err != nil {
		t.Fatal(err)
	}

	ctx := tenancy.WithTenant(t.Context(), acme)
	err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := svc.Upload(ctx, held(tx), contracts.Upload{
			Name: "frame.jpg", ContentType: "image/jpeg",
			Body: bytes.NewReader(picture.Bytes()), Declared: int64(picture.Len()),
		})
		return err
	})
	if !errors.Is(err, contracts.ErrQuota) {
		t.Fatalf("an upload past the quota = %v, want ErrQuota", err)
	}
	if got := keysUnder(t, dir); len(got) != 0 {
		t.Errorf("the refused upload left %v in the store, want no byte of it: the object the row would have named is the one a refusal owes the removal of", got)
	}
}
