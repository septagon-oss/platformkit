package file_test

import (
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

// TestTheReceiptAnUploadPublishesAgreesWithTheRowAtFiled asks that the delivery row an
// upload leaves says the file the row filed, field by field.
//
// The payload is what a subscriber reads instead of reading files back, so every field
// it names has to be the row's own value: the name, media type, size, digest and
// visibility of the object this module stored rather than of what the form declared.
// The row is read back beside the receipt and compared to it, so the case asks for the
// matching and not for today's values, and it refuses only a receipt that stopped
// telling the truth about the file it names. The digest and the visibility are the two
// fields no count of rows can see go missing: a receipt with a blank digest names a
// file its subscriber cannot verify, and one with a blank visibility claims a file
// whose readers the row never named.
func TestTheReceiptAnUploadPublishesAgreesWithTheRowAtFiled(t *testing.T) {
	router, admin, _ := mountedOn(t, map[string]tenancy.Tenant{host: acme})
	code, out := upload(t, router, files, "receipt-of-an-upload.txt", "text/plain", "the bytes one upload filed\n")
	if code != http.StatusCreated {
		t.Fatalf("filing a file = %d %s", code, out)
	}
	id := field(t, out, "id")

	var receiptID, published, rName, rType, rSize, rDigest, rVisibility string
	err := admin.QueryRowContext(t.Context(), `SELECT payload->>'fileId', name, payload->>'name',
		payload->>'contentType', payload->>'size', payload->>'sha256', payload->>'visibility'
		FROM `+outbox+` WHERE payload->>'fileId' = $1`, id).
		Scan(&receiptID, &published, &rName, &rType, &rSize, &rDigest, &rVisibility)
	if err != nil {
		t.Fatalf("reading the %q row this upload left: %v", contracts.EventUploaded, err)
	}
	if receiptID != id {
		t.Errorf("the receipt is filed against %q, want the id the door answered with, %q", receiptID, id)
	}

	var name, contentType, size, digest, visibility string
	if err := admin.QueryRowContext(t.Context(),
		`SELECT name, content_type, size::text, sha256, visibility FROM files WHERE id = $1`, id).
		Scan(&name, &contentType, &size, &digest, &visibility); err != nil {
		t.Fatalf("reading the row the upload filed: %v", err)
	}
	if published != contracts.EventUploaded {
		t.Errorf("the row naming this file is published as %q, want %q", published, contracts.EventUploaded)
	}
	for _, pair := range [][3]string{
		{"name", rName, name},
		{"media type", rType, contentType},
		{"size", rSize, size},
		{"digest", rDigest, digest},
		{"visibility", rVisibility, visibility},
	} {
		if pair[1] != pair[2] {
			t.Errorf("the receipt carries %s %q, want the row's %q", pair[0], pair[1], pair[2])
		}
	}
}
