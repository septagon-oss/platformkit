package file_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// TestTheReceiptsATenantIsAnsweredWithCarryNoDigestThatTenantNeverWrote asks that
// the digest a tenant's subscribers are handed is the digest of the bytes that
// tenant sent, and never the digest of the object another tenant filed.
//
// A file id and a name say whose file a receipt is about; the digest says which
// bytes it describes, and it is the payload field a subscriber acts on without
// reading anything back, because the subscriber this payload exists for is the one
// that indexes or scans the upload. A receipt that carries a stranger's digest
// therefore hands out content rather than a reference to it. The digests below are
// written down from the bytes each upload sent rather than read off the row the
// receipt is meant to agree with, so the case does not ask the row and the receipt
// to be wrong in the same way, and each tenant is answered from its own
// transaction, where the delivery policy binds.
func TestTheReceiptsATenantIsAnsweredWithCarryNoDigestThatTenantNeverWrote(t *testing.T) {
	router, _, conn := mountedOn(t, map[string]tenancy.Tenant{host: acme, otherHost: globex})
	// Neither part claims to be an image, so what the module files is the digest of
	// the bytes it was handed.
	bodies := [][]byte{
		[]byte("the bytes one tenant filed\n"),
		[]byte("the bytes the other tenant filed\n"),
	}
	wrote := []struct {
		digest string
		filed  tenancy.Tenant
	}{
		{digest: digestOf(bodies[0]), filed: acme},
		{digest: digestOf(bodies[1]), filed: globex},
	}
	uploadAs(t, router, host, files, "the-body-acme-wrote.txt", "text/plain", bodies[0])
	uploadAs(t, router, otherHost, files, "the-body-globex-wrote.txt", "text/plain", bodies[1])

	for at, watching := range wrote {
		stranger := wrote[1-at]
		var own, other int
		if err := db.Run(tenancy.WithTenant(t.Context(), watching.filed), conn,
			func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				return tx.DB().Raw(`SELECT count(*) FILTER (WHERE payload->>'sha256' = ?),
						count(*) FILTER (WHERE payload->>'sha256' = ?)
					FROM `+outbox, watching.digest, stranger.digest).Row().Scan(&own, &other)
			}); err != nil {
			t.Fatalf("reading the receipts %s is answered with: %v", watching.filed.Slug, err)
		}
		if own != 1 {
			t.Errorf("%s is answered with %d receipts carrying the digest %s of the bytes it filed, want one",
				watching.filed.Slug, own, watching.digest)
		}
		if other != 0 {
			t.Errorf("%s is handed a receipt carrying the digest %s, of bytes %s filed",
				watching.filed.Slug, stranger.digest, stranger.filed.Slug)
		}
	}
}

// digestOf is the hex digest the module files for bytes it stores as they arrived.
func digestOf(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
