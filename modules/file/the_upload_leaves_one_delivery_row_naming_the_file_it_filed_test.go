package file_test

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

// TestTheUploadLeavesOneDeliveryRowNamingTheFileItFiled counts the delivery rows one
// upload leaves that name the file it filed, and asks that they are one and the upload.
//
// Seven of this module's event payloads carry a fileId (contracts/events.go), and every
// case that reads a receipt this way asks the delivery table by that key alone and takes
// the first row it is answered with — the table has no natural order, its rows carry a
// clock_timestamp() written per statement. So the count here is what makes those reads
// mean the upload rather than the row that happened to come back first: a second event
// that named the file while the upload was still committing would leave such a case
// agreeing with whichever row Postgres chose. The tenant the row names is asked for too,
// because a subscriber reads one tenant's events and a row naming no tenant reaches none
// of them. The body filed is a raster rather than a text body, so the count covers the
// upload that leaves the arrived object behind for a re-encoded one.
func TestTheUploadLeavesOneDeliveryRowNamingTheFileItFiled(t *testing.T) {
	router, admin, _ := mountedOn(t, map[string]tenancy.Tenant{host: acme})
	code, out := upload(t, router, files, "one-tile.gif", "image/gif", string(tileFrame(t)))
	if code != http.StatusCreated {
		t.Fatalf("filing a frame = %d %s", code, out)
	}
	id := field(t, out, "id")

	rows, err := admin.QueryContext(t.Context(),
		`SELECT name, tenant_id = $2 FROM `+outbox+` WHERE payload->>'fileId' = $1`, id, acme.ID)
	if err != nil {
		t.Fatalf("reading the delivery rows that name %s: %v", id, err)
	}
	defer rows.Close()
	var named []string
	for rows.Next() {
		var name string
		var ofAcme bool
		if err := rows.Scan(&name, &ofAcme); err != nil {
			t.Fatalf("reading a delivery row that names %s: %v", id, err)
		}
		if !ofAcme {
			t.Errorf("the delivery row %q names a tenant other than the one that filed the frame", name)
		}
		named = append(named, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("reading the delivery rows that name %s: %v", id, err)
	}
	if len(named) != 1 {
		t.Fatalf("one upload left %d delivery rows naming %s: %v", len(named), id, named)
	}
	if named[0] != contracts.EventUploaded {
		t.Errorf("the one delivery row naming the upload is %q, want %q", named[0], contracts.EventUploaded)
	}
}

// tileFrame is a 4x4 GIF with no transparent pixel: alpha decides which container the
// image pass re-encodes into, and this frame carries none.
func tileFrame(t *testing.T) []byte {
	t.Helper()
	frame := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for x := range 4 {
		for y := range 4 {
			frame.SetNRGBA(x, y, color.NRGBA{R: uint8(20 + x*50), G: uint8(180 - y*30), B: 120, A: 255})
		}
	}
	out := &bytes.Buffer{}
	if err := gif.Encode(out, frame, nil); err != nil {
		t.Fatalf("encoding the frame: %v", err)
	}
	return out.Bytes()
}
