package filetest

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

// The image pass records the stored frame and chooses its container from the pixels.
func reencodeImage(t *testing.T, f Fixture) {
	sent := pngFrame(t, 2, 3)
	var row *contracts.File
	f.one(t, "uploading an image", contracts.EventUploaded, func() {
		out, err := f.Service.Upload(f.Ctx, f.open, asImage(t, "image.png", "image/png", sent))
		if err != nil {
			t.Fatalf("Upload: %v", err)
		}
		row = out
	})
	if row.Width != 2 || row.Height != 3 {
		t.Errorf("the row carries %d x %d, want 2 x 3: nobody measured the frame", row.Width, row.Height)
	}
	if row.SHA256 == "" {
		t.Error("the digest is empty")
	}

	// What nobody called an image stays the attachment it arrived as,
	// even when its header claims a frame: refusing it here would change
	// what a document door accepts, and 0069 §4 does not ask for that.
	if row.ContentType != "image/jpeg" {
		t.Errorf("an opaque frame was stored as %s, want image/jpeg: the container follows the pixels", row.ContentType)
	}
	if _, text := read(t, f, row.ID, false); text == string(sent) {
		t.Error("the stored bytes are the ones that arrived, so nothing was re-encoded")
	} else if bytes.Contains([]byte(text), []byte("Exif")) {
		t.Error("the stored bytes carry an Exif block, which is what the pass removes")
	}

	if doc, err := f.Service.Upload(f.Ctx, f.open, upload("frame.png", "image/png", contracts.VisibilityPrivate, string(pngFrame(t, 2, 2)))); err != nil {
		t.Fatalf("the same bytes as a document: %v", err)
	} else if doc.Width != 2 {
		t.Errorf("an unclaimed image carries %d px, want the pass to have run over it too", doc.Width)
	}
}

// An undecodable image is refused without retaining bytes.
func refuseUndecodableImage(t *testing.T, f Fixture) {
	before := len(f.Keys())
	_, err := f.Service.Upload(f.Ctx, f.open, asImage(t, "diagram.svg", "image/svg+xml",
		[]byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)))
	if !errors.Is(err, contracts.ErrNotImage) {
		t.Errorf("a vector offered as an image = %v, want ErrNotImage", err)
	}
	if err == nil || !strings.Contains(err.Error(), "not an image") {
		t.Errorf("the refusal does not say the reason: %v", err)
	}
	if len(f.Keys()) != before {
		t.Errorf("the refusal left %d objects behind, want none", len(f.Keys())-before)
	}
	if len(f.Published()) != 0 {
		t.Errorf("the refusal published %v, want nothing", f.Published())
	}
}
