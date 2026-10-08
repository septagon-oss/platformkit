package contracts

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
)

// jpegWithExif wraps an encoded JPEG in an APP1 Exif block carrying the given
// orientation and a GPS sub-IFD, which is what a phone writes.
//
// It is written by hand rather than with a library because the thing under test
// is what survives a pass over a file somebody else produced: a fixture an
// encoder of ours made would prove the fixture as much as the code.
func jpegWithExif(t *testing.T, frame image.Image, orientation int, littleEndian bool) []byte {
	t.Helper()
	body := &bytes.Buffer{}
	if err := jpeg.Encode(body, frame, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	var order binary.ByteOrder = binary.BigEndian
	marker := []byte("MM")
	if littleEndian {
		order = binary.LittleEndian
		marker = []byte("II")
	}
	// IFD0 at offset 8: the orientation, whose value is one short and so sits in
	// the entry itself. The GPS sub-IFD follows at 26 and carries a version tag
	// spelled "GPS" and one latitude string — enough bytes for the case to prove
	// something survived, and none of them read by the pass.
	tiff := &bytes.Buffer{}
	tiff.Write(marker)
	_ = binary.Write(tiff, order, uint16(0x002A))
	_ = binary.Write(tiff, order, uint32(8))
	_ = binary.Write(tiff, order, uint16(1))
	_ = binary.Write(tiff, order, uint16(0x0112))
	_ = binary.Write(tiff, order, uint16(3))
	_ = binary.Write(tiff, order, uint32(1))
	_ = binary.Write(tiff, order, uint16(orientation))
	_ = binary.Write(tiff, order, uint16(0))
	_ = binary.Write(tiff, order, uint32(26))
	_ = binary.Write(tiff, order, uint16(2))
	_ = binary.Write(tiff, order, uint16(0x0000)) // GPSVersionID
	_ = binary.Write(tiff, order, uint16(7))      // UNDEFINED
	_ = binary.Write(tiff, order, uint32(4))
	tiff.Write([]byte("GPS\x00"))
	_ = binary.Write(tiff, order, uint16(0x0002)) // GPSLatitude, an ASCII value behind an offset
	_ = binary.Write(tiff, order, uint16(2))
	_ = binary.Write(tiff, order, uint32(11))
	_ = binary.Write(tiff, order, uint32(44))
	_ = binary.Write(tiff, order, uint32(0))
	tiff.WriteString("5234.1234N\x00")
	exif := append([]byte("Exif\x00\x00"), tiff.Bytes()...)
	// The APP1 length counts itself and the two marker bytes, and the segment
	// sits between the start of image and the encoder's own JFIF block.
	out := []byte{0xFF, 0xD8, 0xFF, 0xE1}
	segLen := len(exif) + 2
	out = append(out, byte(segLen/256), byte(segLen%256))
	out = append(out, exif...)
	return append(out, body.Bytes()[2:]...)
}

// TestPassWritesNoMetadataBack is the acceptance item: a JPEG carrying EXIF and
// a GPS sub-IFD goes in, and no stored byte carries either of them.
func TestPassWritesNoMetadataBack(t *testing.T) {
	frame := image.NewRGBA(image.Rect(0, 0, 4, 2))
	for x := 0; x < 4; x++ {
		frame.Set(x, 0, color.RGBA{R: 200, A: 255})
	}
	in := jpegWithExif(t, frame, 1, false)
	if got := exifOrientation(in); got != 1 {
		t.Fatalf("the fixture itself carries no orientation, so the case would prove nothing: %d", got)
	}
	if !bytes.Contains(in, []byte("GPS")) {
		t.Fatal("the fixture carries no GPS block, so the case would prove nothing")
	}
	pass, err := ProcessImage(bytes.NewReader(in), 40_000_000)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(pass.Bytes, []byte("Exif")) || bytes.Contains(pass.Bytes, []byte("GPS")) {
		t.Errorf("the stored bytes still name metadata: %q", firstKeywords(pass.Bytes))
	}
	if got := exifOrientation(pass.Bytes); got != 0 {
		t.Errorf("the stored JPEG still carries an orientation tag: %d", got)
	}
	if _, format, err := image.DecodeConfig(bytes.NewReader(pass.Bytes)); err != nil || format != "jpeg" {
		t.Errorf("the stored bytes are not a JPEG a reader can open: %s %v", format, err)
	}
	if pass.Width != 4 || pass.Height != 2 {
		t.Errorf("dimensions = %d x %d, want 4 x 2", pass.Width, pass.Height)
	}
}

// TestRotatedFrameIsStoredUpright is the other half of the same acceptance
// item: the pixels are turned as the tag says, and the dimensions describe the
// frame that is stored rather than the one the sensor captured.
func TestRotatedFrameIsStoredUpright(t *testing.T) {
	// A 4 x 2 frame with one red pixel in the top-left corner. Rotated 90
	// degrees clockwise, that corner is the top-right of a 2 x 4 frame.
	frame := halfAndHalf()
	in := jpegWithExif(t, frame, 6, true)
	if got := exifOrientation(in); got != 6 {
		t.Fatalf("little-endian orientation = %d, want 6", got)
	}
	pass, err := ProcessImage(bytes.NewReader(in), 40_000_000)
	if err != nil {
		t.Fatal(err)
	}
	if pass.Width != 32 || pass.Height != 64 {
		t.Fatalf("stored dimensions = %d x %d, want 32 x 64: the rotation was not applied", pass.Width, pass.Height)
	}
	back, err := jpeg.Decode(bytes.NewReader(pass.Bytes))
	if err != nil {
		t.Fatal(err)
	}
	// A bright half turned ninety degrees clockwise is a bright half on top, so
	// the rows are compared rather than any one pixel: a JPEG re-encode moves a
	// chroma block, and a test that read one pixel would be testing the encoder.
	if top, bottom := meanRow(back, 0), meanRow(back, back.Bounds().Dy()-1); top <= bottom+40 {
		t.Errorf("the frame was not turned: its top row reads %d and its bottom row %d", top, bottom)
	}
}

func TestOrientationOfEveryTag(t *testing.T) {
	for _, o := range []int{1, 2, 3, 4, 5, 6, 7, 8} {
		frame := image.NewRGBA(image.Rect(0, 0, 3, 2))
		frame.Set(0, 0, color.RGBA{R: 255, A: 255})
		for _, le := range []bool{false, true} {
			in := jpegWithExif(t, frame, o, le)
			if got := exifOrientation(in); got != o {
				t.Errorf("orientation %d (%+v endian) = %d", o, le, got)
			}
		}
	}
	// A file with no APP1 at all, and a file whose APP1 is not Exif, are both
	// "the camera said nothing", which is orientation 0 and no transform.
	if got := exifOrientation([]byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x02, 0x00, 0x00}); got != 0 {
		t.Errorf("a JFIF-only file = %d, want 0", got)
	}
	if got := exifOrientation(nil); got != 0 {
		t.Errorf("nothing at all = %d, want 0", got)
	}
}

// TestFrameOverTheCeilingIsRefusedByName is the decompression bomb: the answer
// arrives at the header, and the reason names the pixels rather than a size.
func TestFrameOverTheCeilingIsRefusedByName(t *testing.T) {
	in := pngHeader(t, 10000, 6000) // 60 megapixels, eight and a half lines
	_, err := ProcessImage(bytes.NewReader(in), 40_000_000)
	if !isTooManyPixels(err) {
		t.Fatalf("a 60 MP frame = %v, want a pixel-ceiling refusal", err)
	}
	if !strings.Contains(err.Error(), "60000000") || !strings.Contains(err.Error(), "40000000") {
		t.Errorf("the refusal does not name the count: %v", err)
	}
	// Exactly the ceiling is not over it.
	if err := refusePixels(8000, 5000, 40_000_000); err != nil {
		t.Errorf("a frame of exactly the ceiling was refused: %v", err)
	}
}

// TestFrameOverTheCeilingNeverAllocates is why the ceiling is read off the
// header: the object below is a PNG header and nothing else, so a pass that
// decoded before it measured would have a different shape entirely.
func TestFrameOverTheCeilingNeverAllocates(t *testing.T) {
	if _, err := ProcessImage(bytes.NewReader(pngHeader(t, 70000, 70000)), 40_000_000); !isTooManyPixels(err) {
		t.Fatalf("an 4,900 MP header = %v", err)
	}
}

// TestVectorIsRefusedBeforeAByteIsStored is how an SVG with a script in it
// dies: it is offered as an image, and no decoder this pass runs reads it.
func TestVectorIsRefusedBeforeAByteIsStored(t *testing.T) {
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
	_, err := ProcessImage(bytes.NewReader(svg), 40_000_000)
	if !isNotImage(err) {
		t.Fatalf("an SVG = %v, want a refusal that says it is not an image", err)
	}
	if !strings.Contains(err.Error(), "not an image") {
		t.Errorf("the refusal does not name the reason: %v", err)
	}
	// Truncated to a header a decoder accepts and no more: still refused, and
	// still as not-an-image rather than as a decode failure.
	if _, err := ProcessImage(bytes.NewReader(pngHeader(t, 8, 8)), 40_000_000); !isNotImage(err) {
		t.Errorf("a PNG header with no image behind it = %v", err)
	}
}

// TestAlphaDecidesTheContainer keeps the encoder's choice where it can be read:
// a frame that carries transparency stays a PNG, and one that does not becomes
// a JPEG rather than three times the bytes.
func TestAlphaDecidesTheContainer(t *testing.T) {
	opaque := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			opaque.Set(x, y, color.RGBA{R: uint8(x * 30), G: uint8(y * 30), B: 40, A: 255})
		}
	}
	pass, err := ProcessImage(bytes.NewReader(pngOf(t, opaque)), 40_000_000)
	if err != nil {
		t.Fatal(err)
	}
	if pass.ContentType != "image/jpeg" {
		t.Errorf("an opaque frame was stored as %s, want image/jpeg", pass.ContentType)
	}

	seeThrough := image.NewRGBA(opaque.Bounds())
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			seeThrough.Set(x, y, opaque.At(x, y))
		}
	}
	seeThrough.Set(3, 3, color.RGBA{R: 10, G: 10, B: 10, A: 0})
	pass, err = ProcessImage(bytes.NewReader(pngOf(t, seeThrough)), 40_000_000)
	if err != nil {
		t.Fatal(err)
	}
	if pass.ContentType != "image/png" {
		t.Errorf("a frame with one transparent pixel was stored as %s, want image/png", pass.ContentType)
	}
}

// TestGifKeepsItsShapeAcrossThePass: a GIF is a paletted frame, and the pass
// reads pixels rather than the container they arrived in.
func TestGifKeepsItsShapeAcrossThePass(t *testing.T) {
	frame := image.NewPaletted(image.Rect(0, 0, 6, 3), color.Palette{color.Black, color.RGBA{R: 255, A: 255}})
	frame.SetColorIndex(0, 0, 1)
	in := &bytes.Buffer{}
	if err := gif.Encode(in, frame, nil); err != nil {
		t.Fatal(err)
	}
	pass, err := ProcessImage(bytes.NewReader(in.Bytes()), 40_000_000)
	if err != nil {
		t.Fatal(err)
	}
	if pass.Width != 6 || pass.Height != 3 {
		t.Errorf("dimensions = %d x %d, want 6 x 3", pass.Width, pass.Height)
	}
	if pass.ContentType != "image/jpeg" {
		t.Errorf("an opaque GIF was stored as %s, want image/jpeg", pass.ContentType)
	}
}

func isTooManyPixels(err error) bool {
	return errors.Is(err, ErrTooManyPixels)
}

func isNotImage(err error) bool {
	return errors.Is(err, ErrNotImage)
}

func pngOf(t *testing.T, m image.Image) []byte {
	t.Helper()
	out := &bytes.Buffer{}
	if err := png.Encode(out, m); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

// pngHeader writes a PNG file that declares a frame of w x h and holds nothing
// else: eight bytes of signature, one IHDR chunk, no pixels and no IEND.
func pngHeader(t *testing.T, w, h int) []byte {
	t.Helper()
	out := &bytes.Buffer{}
	out.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1A, '\n'})
	chunk := &bytes.Buffer{}
	_ = binary.Write(chunk, binary.BigEndian, uint32(w))
	_ = binary.Write(chunk, binary.BigEndian, uint32(h))
	chunk.Write([]byte{8, 6, 0, 0, 0})
	body := chunk.Bytes()
	_ = binary.Write(out, binary.BigEndian, uint32(len(body)))
	out.Write([]byte("IHDR"))
	out.Write(body)
	// Every PNG chunk carries its own CRC over the type and the data, and a
	// decoder that reads a wrong one stops there: the header below has to be a
	// header a decoder agrees to read, because what is under test is the
	// ceiling and not the checksum.
	_ = binary.Write(out, binary.BigEndian, crc32.ChecksumIEEE(append(append([]byte{}, []byte("IHDR")...), body...)))
	return out.Bytes()
}

// halfAndHalf is a 64 x 32 frame whose left half is white and whose right half
// is black: a shape that says which way is up from more than one pixel.
func halfAndHalf() *image.RGBA {
	frame := image.NewRGBA(image.Rect(0, 0, 64, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 64; x++ {
			frame.Set(x, y, color.RGBA{A: 255})
			if x < 32 {
				frame.Set(x, y, color.RGBA{R: 255, G: 255, B: 255, A: 255})
			}
		}
	}
	return frame
}

// meanRow is the average of one row, so a comparison survives an encoder that
// does not reproduce an edge exactly.
func meanRow(src image.Image, y int) int {
	b := src.Bounds()
	total := 0
	for x := b.Min.X; x < b.Max.X; x++ {
		r, g, bl, _ := src.At(x, y).RGBA()
		total += int((r + g + bl) / (3 * 256))
	}
	return total / b.Dx()
}

func firstKeywords(b []byte) string {
	for _, k := range []string{"Exif", "GPS", "http://ns.adobe.com"} {
		if bytes.Contains(b, []byte(k)) {
			return k
		}
	}
	return ""
}
