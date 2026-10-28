package file_test

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"net/http"
	"strconv"
	"testing"

	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// TestTheReceiptNamesTheMediaTypeTheObjectWasFiledAs asks the receipt about the one
// upload whose media type, size and digest change on the way in.
//
// A raster is re-encoded before it is kept (contracts.ProcessImage), so the object a
// reader fetches is not the one the form sent: for the opaque frame below the stored
// container is a JPEG, and its length and digest are the re-encoded bytes' rather
// than the arrived ones. The delivery row is what a subscriber reads instead of
// reading the file back, so it has to name the filed object and not the declaration —
// a receipt built from what the form said would tell a subscriber to expect a GIF of
// the arrived length at a digest no object answers to. The row is read back beside
// the receipt and compared to it, and the arrived type and length are named as what
// the receipt must not say, so this refuses the slip rather than today's container.
func TestTheReceiptNamesTheMediaTypeTheObjectWasFiledAs(t *testing.T) {
	router, admin, _ := mountedOn(t, map[string]tenancy.Tenant{host: acme})
	arrived := opaqueFrame(t)
	code, out := upload(t, router, files, "one-frame.gif", "image/gif", string(arrived))
	if code != http.StatusCreated {
		t.Fatalf("filing a frame = %d %s", code, out)
	}
	id := field(t, out, "id")

	var rType, rSize, rDigest string
	if err := admin.QueryRowContext(t.Context(),
		`SELECT payload->>'contentType', payload->>'size', payload->>'sha256' FROM `+outbox+`
		WHERE payload->>'fileId' = $1`, id).Scan(&rType, &rSize, &rDigest); err != nil {
		t.Fatalf("reading the receipt this upload left: %v", err)
	}
	var name, contentType, size, digest string
	if err := admin.QueryRowContext(t.Context(),
		`SELECT name, content_type, size::text, sha256 FROM files WHERE id = $1`, id).
		Scan(&name, &contentType, &size, &digest); err != nil {
		t.Fatalf("reading the row the upload filed: %v", err)
	}
	for _, pair := range [][3]string{
		{"media type", rType, contentType},
		{"size", rSize, size},
		{"digest", rDigest, digest},
	} {
		if pair[1] != pair[2] {
			t.Errorf("the receipt carries %s %q, want the filed row's %q", pair[0], pair[1], pair[2])
		}
	}
	if rType == "image/gif" {
		t.Errorf("the receipt names the media type the form declared (%q) rather than the %q the object was filed as", rType, contentType)
	}
	if rSize == strconv.Itoa(len(arrived)) {
		t.Errorf("the receipt names the %s bytes that arrived rather than the %s the frame was filed as", rSize, size)
	}
}

// opaqueFrame is a 4x4 GIF with no transparent pixel, which is the frame the re-encode
// turns into a JPEG: alpha decides the container, and this one carries none.
func opaqueFrame(t *testing.T) []byte {
	t.Helper()
	frame := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for x := range 4 {
		for y := range 4 {
			frame.SetNRGBA(x, y, color.NRGBA{R: uint8(30 + x*40), G: uint8(200 - y*20), B: 90, A: 255})
		}
	}
	out := &bytes.Buffer{}
	if err := gif.Encode(out, frame, nil); err != nil {
		t.Fatalf("encoding the frame: %v", err)
	}
	return out.Bytes()
}
