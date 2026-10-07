package contracts

import (
	"bytes"
	"image"
	"math/rand/v2"
	"testing"
)

// noisy is a frame no encoder can shrink much, so its file is the size a
// camera's is: well past the first megabyte of the object.
func noisy(w, h int) *image.RGBA {
	r := rand.New(rand.NewPCG(1, 2))
	m := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := range m.Pix {
		m.Pix[i] = byte(r.IntN(256))
	}
	for i := 3; i < len(m.Pix); i += 4 {
		m.Pix[i] = 255
	}
	return m
}

// TestAPhotoPastTheFirstMegabyteIsDecodedWhole: a photograph is several
// megabytes, and the pass reads all of it — the stored frame is the whole
// frame, measured, and the GPS block it arrived with is gone.
func TestAPhotoPastTheFirstMegabyteIsDecodedWhole(t *testing.T) {
	in := jpegWithExif(t, noisy(1600, 1200), 1, false)
	if len(in) <= headerProbe {
		t.Fatalf("the fixture is %d bytes, not past the first megabyte, so the case would prove nothing", len(in))
	}
	if !bytes.Contains(in, []byte("GPS")) {
		t.Fatal("the fixture carries no GPS block, so the case would prove nothing")
	}
	pass, err := ProcessImage(bytes.NewReader(in), DefaultMaxImagePixels)
	if err != nil {
		t.Fatalf("a %d-byte JPEG of 1600 x 1200 was not read: %v", len(in), err)
	}
	if pass.Width != 1600 || pass.Height != 1200 {
		t.Errorf("dimensions = %d x %d, want 1600 x 1200", pass.Width, pass.Height)
	}
	// Metadata lives in the segments before the scan; the scan itself is noise
	// and may spell anything, so only the header is read.
	sos := bytes.Index(pass.Bytes, []byte{0xFF, 0xDA})
	if sos < 0 {
		t.Fatal("the stored JPEG has no start of scan")
	}
	if header := pass.Bytes[:sos]; bytes.Contains(header, []byte("GPS")) || bytes.Contains(header, []byte("Exif")) ||
		bytes.Contains(header, []byte{0xFF, 0xE1}) {
		t.Errorf("the stored header still carries metadata: %q", firstKeywords(header))
	}
	if got := exifOrientation(pass.Bytes); got != 0 {
		t.Errorf("the stored JPEG still carries an orientation tag: %d", got)
	}
	big := pngOf(t, noisy(800, 600))
	if len(big) <= headerProbe {
		t.Fatalf("the PNG fixture is %d bytes, not past the first megabyte", len(big))
	}
	pass, err = ProcessImage(bytes.NewReader(big), DefaultMaxImagePixels)
	if err != nil {
		t.Fatalf("a %d-byte PNG of 800 x 600 was not read: %v", len(big), err)
	}
	if pass.Width != 800 || pass.Height != 600 {
		t.Errorf("PNG dimensions = %d x %d, want 800 x 600", pass.Width, pass.Height)
	}
}
