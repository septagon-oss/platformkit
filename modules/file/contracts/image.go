package contracts

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/draw"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"mime"
	"net/http"
)

// image.go is the pass a raster takes on its way into storage (decision 0069 §4).
//
// Three things are decided here and nowhere else: what the server refuses to
// decode at all, what it keeps after decoding, and what it writes back in place
// of the bytes it was given. The order is the whole point — the header is read
// before a frame is allocated, so a decompression bomb costs a header and not a
// gigabyte, and the re-encode happens from decoded pixels only, so there is no
// path by which a caller's metadata reaches the object store. Nothing here
// parses the metadata it is about to discard beyond the one tag that says how
// the camera held the sensor, because an image that has to be understood to be
// cleaned is an image that can hide something in the reading.
//
// Pure Go and stdlib decoders: JPEG, PNG and the first frame of a GIF. WebP is
// a `golang.org/x/image/webp` require away from this switch, and it is left
// there rather than added here because a new dependency is a `packages-budget`
// and a licence conversation of its own — see T-0188's IMPLEMENT.md.

// jpegQuality is the quality a re-encode writes. 82 is the point where a
// photographic frame is visually indistinguishable from the original at the
// sizes this module serves; it is a constant rather than a knob because a
// deployment that could set it could set it to 30, and then the module
// quietly damages people's photographs.
const jpegQuality = 82

// headerProbe is how much of the object is buffered to read a header out of.
//
// It has to hold the longest realistic JPEG APP1 segment — an EXIF block with a
// thumbnail runs to a few tens of kilobytes, and `image.DecodeConfig` needs the
// start-of-frame marker, which sits after it — and it must stay small enough to
// be held per in-flight upload rather than proportional to the file. One
// megabyte is both.
const headerProbe = 1 << 20

// ImagePass is what one raster became on the way in.
//
// Bytes replaces the object that was stored: the row's size, digest and media
// type are read from it, not from what the request declared, because after this
// pass the bytes a reader fetches are these and not the ones that were sent.
type ImagePass struct {
	Bytes       []byte
	ContentType string
	Width       int
	Height      int
}

// imageFormats is the closed set of decoders this pass runs. A header that
// decodes as anything else — a WebP, an SVG, a TIFF — is not an image as far as
// storage is concerned, which is what refuses an SVG with a <script> in it
// before a byte of it is kept (0069 §4).
func imageFormat(format string) bool {
	switch format {
	case "jpeg", "png", "gif":
		return true
	default:
		return false
	}
}

// probeImage reads the header of a raster out of its first megabyte and answers
// the two questions that can be answered without decoding: how big the frame
// claims to be, and which decoder would be asked to read it.
//
// It returns the bytes it read as well, because the caller needs them for the
// orientation tag and re-reading them from the store would be a second round
// trip for a megabyte nobody asked for.
func probeImage(r io.Reader) (head []byte, width, height int, format string, err error) {
	head, err = io.ReadAll(io.LimitReader(r, headerProbe))
	if err != nil {
		return nil, 0, 0, "", fmt.Errorf("file: read image header: %w", err)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(head))
	if err != nil {
		return head, 0, 0, "", fmt.Errorf("%w: %v", ErrNotImage, err)
	}
	if !imageFormat(format) {
		return head, 0, 0, format, fmt.Errorf("%w: a %s is not an image this deployment decodes", ErrNotImage, format)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return head, 0, 0, format, fmt.Errorf("%w: it claims a frame of %d by %d", ErrNotImage, cfg.Width, cfg.Height)
	}
	return head, cfg.Width, cfg.Height, format, nil
}

// refusePixels is the ceiling, stated as the sentence a caller can act on.
//
// It is a count of pixels and not of bytes because the two come apart in exactly
// one direction: a file can be small and a frame enormous, which is the
// decompression bomb, and the only number that bounds the allocation a decode
// will make is width times height.
func refusePixels(width, height, maxPixels int) error {
	if maxPixels <= 0 || int64(width)*int64(height) <= int64(maxPixels) {
		return nil
	}
	return fmt.Errorf("%w: %d x %d is %d pixels, more than the %d this deployment decodes",
		ErrTooManyPixels, width, height, int64(width)*int64(height), maxPixels)
}

// ProcessImage is the whole pass: header, ceiling, decode, orientation,
// re-encode. On return the caller has the object to store instead of the one it
// was handed, and the dimensions to record beside it.
//
// The ceiling is checked against the header before the frame exists, so the
// peak allocation this function makes is one decoded frame at or below the
// ceiling plus the encoded output — which is why the ceiling is a pixel count
// and a deployment's setting rather than a fixed constant.
func ProcessImage(r io.Reader, maxPixels int) (ImagePass, error) {
	head, width, height, _, err := probeImage(r)
	if err != nil {
		return ImagePass{}, err
	}
	if err := refusePixels(width, height, maxPixels); err != nil {
		return ImagePass{}, err
	}
	// The decoded frame, and not the bytes: everything the caller sent that is
	// not pixels is dropped here, because this is the only read of the object
	// whose output is stored, and it reads pixels and nothing else.
	//
	// The header is re-fed to the decoder behind what it buffered, so the
	// decoder sees the object from its first byte without a second fetch.
	src, format, err := image.Decode(io.TeeReader(bytes.NewReader(head), io.Discard))
	if err != nil {
		return ImagePass{}, fmt.Errorf("%w: a %s header with bytes no decoder reads: %v", ErrNotImage, format, err)
	}
	frame := orient(toRGBA(src), exifOrientation(head))
	// Alpha decides the container, and it is a scan of the frame rather than a
	// filename, a declared type or a tRNS-chunk guess: a GIF that carries
	// transparency re-encoded to JPEG loses it, and a photograph that carries
	// none re-encoded to PNG costs three times the bytes for nothing.
	out := &bytes.Buffer{}
	if hasAlpha(frame) {
		if err := png.Encode(out, frame); err != nil {
			return ImagePass{}, fmt.Errorf("file: re-encode %s as PNG: %w", format, err)
		}
		return ImagePass{Bytes: out.Bytes(), ContentType: "image/png", Width: frame.Bounds().Dx(), Height: frame.Bounds().Dy()}, nil
	}
	if err := jpeg.Encode(out, frame, &jpeg.Options{Quality: jpegQuality}); err != nil {
		return ImagePass{}, fmt.Errorf("file: re-encode %s as JPEG: %w", format, err)
	}
	return ImagePass{Bytes: out.Bytes(), ContentType: "image/jpeg", Width: frame.Bounds().Dx(), Height: frame.Bounds().Dy()}, nil
}

// toRGBA copies a decoded frame into one pixel layout, because the orientation
// transform, the alpha scan and both encoders all want the same one, and the
// decoders hand back whatever was cheapest for them: RGBA for a JPEG, a
// paletted image for a GIF, sometimes a YCbCr sub-sampled image.
func toRGBA(src image.Image) *image.RGBA {
	b := src.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(out, out.Bounds(), src, b.Min, draw.Src)
	return out
}

// orient returns the frame the way a reader of the file would see it, applying
// the EXIF orientation the camera recorded.
//
// Doing this at write time rather than leaving the tag for a viewer is what
// makes "stored upright" testable at all: a viewer that honours
// `image-orientation: from-image` shows a rotated photo correctly, but the
// pixels, the dimensions on the row and every variant derived from them are
// still the sideways ones, and the crop a screen does next is wrong.
func orient(src *image.RGBA, orientation int) *image.RGBA {
	if orientation < 2 || orientation > 8 {
		return src
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	if orientation >= 5 {
		dst = image.NewRGBA(image.Rect(0, 0, h, w))
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var p image.Point
			switch orientation {
			case 2:
				p = image.Point{X: w - 1 - x, Y: y}
			case 3:
				p = image.Point{X: w - 1 - x, Y: h - 1 - y}
			case 4:
				p = image.Point{X: x, Y: h - 1 - y}
			case 5:
				p = image.Point{X: y, Y: x}
			case 6:
				p = image.Point{X: h - 1 - y, Y: x}
			case 7:
				p = image.Point{X: h - 1 - y, Y: w - 1 - x}
			case 8:
				p = image.Point{X: y, Y: w - 1 - x}
			}
			dst.SetRGBA(p.X, p.Y, src.RGBAAt(x, y))
		}
	}
	return dst
}

// hasAlpha reports whether any pixel carries transparency.
func hasAlpha(src *image.RGBA) bool {
	for i := 3; i < len(src.Pix); i += 4 {
		if src.Pix[i] != 0xFF {
			return true
		}
	}
	return false
}

// exifOrientation reads tag 0x0112 out of a JPEG's APP1 Exif block, over
// encoding/binary, and answers 0 for every case where it is not there or not
// legible.
//
// It reads that one tag and nothing else, on purpose: it is the only fact about
// a file's metadata that changes the bytes this module stores, and a parser that
// walked the whole block would be a parser a crafted file could spend. A PNG or
// a GIF carries no such tag, and the loop below simply finds no APP1 and stops.
func exifOrientation(data []byte) int {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return 0
	}
	for i := 2; i+4 <= len(data); {
		if data[i] != 0xFF {
			return 0 // no padding, no marker: this is not a segment stream
		}
		marker := data[i+1]
		if marker == 0x01 || marker == 0xD8 || (marker >= 0xD0 && marker <= 0xD7) {
			i += 2 // standalone markers carry no length
			continue
		}
		if marker == 0xDA || marker == 0xD9 {
			return 0 // start of scan, or end of image: no metadata follows
		}
		segLen := int(data[i+2])<<8 | int(data[i+3])
		if segLen < 2 || i+2+segLen > len(data) {
			return 0
		}
		seg := data[i+4 : i+2+segLen]
		if marker == 0xE1 && len(seg) > 6 && bytes.Equal(seg[:6], []byte("Exif\x00\x00")) {
			return tiffOrientation(seg[6:])
		}
		i += 2 + segLen
	}
	return 0
}

// ReadsAsImage decides which uploads the image pass runs over, from what the
// caller said and from what the first bytes are, never from a name. The sniff is
// the same one Agrees runs, so an upload that claimed to be a raster and got
// past that check is measured here too. Both the SQL service and the fake call
// this, and neither re-derives the answer.
func ReadsAsImage(up Upload, head []byte) bool {
	if up.Image {
		return true
	}
	switch sniffedContentType(head) {
	case "image/jpeg", "image/png", "image/gif":
		return true
	}
	return false
}

// sniffedContentType is what the first bytes say, by the sniff Agrees runs. A
// head is never longer than 512 bytes, which is what http.DetectContentType
// reads and what it uses when there is less.
func sniffedContentType(head []byte) string {
	essence, _, err := mime.ParseMediaType(http.DetectContentType(head))
	if err != nil {
		return ""
	}
	return essence
}

// RefusesPass is the one place that decides whether a failed image pass is a
// refusal or a shrug, and the rule is the caller's own declaration: somebody who
// said "image" gets the reason, and a file nobody claimed was one is kept as it
// arrived. 0069 §4 asks what an image door refuses, not what a document door
// accepts, so the two are not allowed to disagree — which is why this is a
// function and not a line in each service.
func RefusesPass(err error, claimed bool) bool {
	return claimed || errors.Is(err, ErrTooManyPixels)
}

// tiffOrientation walks IFD0 of the TIFF block inside an Exif segment for one
// short, honouring the block's own byte order.
func tiffOrientation(tiff []byte) int {
	if len(tiff) < 8 {
		return 0
	}
	var order binary.ByteOrder
	switch string(tiff[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 0
	}
	ifd := int(order.Uint32(tiff[4:8]))
	if ifd < 8 || ifd+2 > len(tiff) {
		return 0
	}
	entries := int(order.Uint16(tiff[ifd : ifd+2]))
	for e := 0; e < entries; e++ {
		at := ifd + 2 + e*12
		if at+12 > len(tiff) {
			return 0
		}
		// A short is two bytes and this tag has exactly one, so the value sits
		// in the entry itself rather than behind an offset: no further read,
		// and therefore nothing for a malformed block to send us to.
		if order.Uint16(tiff[at:at+2]) == 0x0112 {
			if v := int(order.Uint16(tiff[at+8 : at+10])); v >= 1 && v <= 8 {
				return v
			}
			return 0
		}
	}
	return 0
}
