package contracts

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// The plan one frame becomes, asked of the arithmetic rather than of a database:
// what widths exist, how tall each is, and which object name each has. Both
// implementations call these functions, so a rule that fails here fails the SQL
// service and the fake at once — which is the point of the plan living here.

func TestVariantPlanNeverUpscales(t *testing.T) {
	for _, tc := range []struct {
		frame  int
		want   string
		reason string
	}{
		{2400, "480 960 1440 1920", "a frame wider than every served width takes all four"},
		{700, "480", "a frame between two served widths takes the ones below it"},
		{480, "", "a frame exactly one served width gets no variant at that width: the original is that width"},
		{400, "", "a frame smaller than every served width is never scaled up"},
		{0, "", "an unmeasured frame has no plan"},
	} {
		var got []string
		for _, w := range VariantWidths(tc.frame) {
			got = append(got, strconv.Itoa(w))
		}
		if strings.Join(got, " ") != tc.want {
			t.Errorf("VariantWidths(%d) = %q, want %q (%s)", tc.frame, strings.Join(got, " "), tc.want, tc.reason)
		}
	}
}

func TestVariantHeightKeepsTheRatio(t *testing.T) {
	if got := VariantHeight(2400, 1600, 960); got != 640 {
		t.Errorf("a 2400x1600 frame at 960 px is %d tall, want 640", got)
	}
	if got := VariantHeight(2400, 1600, 480); got != 320 {
		t.Errorf("a 2400x1600 frame at 480 px is %d tall, want 320", got)
	}
	// Truncation would answer zero here, and a zero-tall frame encodes to nothing
	// and lays out as nothing.
	if got := VariantHeight(1000, 1, 480); got != 1 {
		t.Errorf("a 1000x1 banner at 480 px is %d tall, want 1 and never 0", got)
	}
	if got := VariantHeight(0, 600, 480); got != 0 {
		t.Errorf("an unmeasured frame has no height at any width, got %d", got)
	}
}

func TestVariantKeyIsDerivedAndDistinct(t *testing.T) {
	file := uuid.New()
	same := VariantKey(file, 480)
	if again := VariantKey(file, 480); again != same {
		t.Fatalf("one (file,width) has two object names: %s and %s", same, again)
	}
	if VariantKey(file, 960) == same {
		t.Fatalf("two widths of one file share the object name %s", same)
	}
	if VariantKey(uuid.New(), 480) == same {
		t.Fatal("two files share a width's object name")
	}
	if _, err := ParseKey(same.String()); err != nil {
		t.Errorf("a derived key must still be a storage key: %v", err)
	}
	// The plan a measured frame files, as rows: one per width, none of them
	// charged, all of them pointing at the name the encode will write.
	plan := VariantPlan(file, 2400, 1600, "image/jpeg")
	if len(plan) != 4 {
		t.Fatalf("a 2400 px frame plans %d widths, want 4", len(plan))
	}
	for _, row := range plan {
		if row.State != VariantPending || row.Size != 0 {
			t.Errorf("planned %d px: state %s, %d bytes; a plan charges for nothing", row.Width, row.State, row.Size)
		}
		if row.StorageKey != VariantKey(file, row.Width).String() {
			t.Errorf("%d px names %s, want the derived %s", row.Width, row.StorageKey, VariantKey(file, row.Width))
		}
		if err := row.Validate(context.Background()); err != nil {
			t.Errorf("a planned %d px row refuses itself: %v", row.Width, err)
		}
	}
}

// A WebP is a header a ceiling can be read out of and a frame a pass can re-encode,
// through the one registration that lets image.DecodeConfig answer it. The two
// refusals that must survive the new decoder are tested beside it: a frame over
// the ceiling named before any pixels are allocated, and a body whose bytes are
// not what it was uploaded as.
func TestAWebPDecodesAndIsMeasuredFromItsHeader(t *testing.T) {
	frame := webpFixture(t)
	pass, err := ProcessImage(bytes.NewReader(frame), DefaultMaxImagePixels)
	if err != nil {
		t.Fatalf("a WebP the pass will not read: %v", err)
	}
	if pass.Width != 75 || pass.Height != 100 {
		t.Errorf("the pass measured %dx%d of a WebP whose header says 75x100", pass.Width, pass.Height)
	}
	if !ReadsAsImage(Upload{}, frame) {
		t.Error("ReadsAsImage does not read a WebP's first bytes as an image")
	}
	// The ceiling, asked of the header alone: this is the acceptance bullet's
	// "refused by name", and it is what a decoder registered after the header gate
	// rather than beside it would silently turn into an allocation.
	head, w, h, format, err := probeImage(bytes.NewReader(frame))
	if err != nil {
		t.Fatalf("the header of a WebP: %v", err)
	}
	if w != 75 || h != 100 || format != "webp" || len(head) == 0 {
		t.Fatalf("the header answered %dx%d as %s", w, h, format)
	}
	err = refusePixels(9000, 7000, DefaultMaxImagePixels)
	if !errors.Is(err, ErrTooManyPixels) {
		t.Fatalf("a 63 MP frame was not refused by name: %v", err)
	}
	if !strings.Contains(err.Error(), "63000000") {
		t.Errorf("the refusal does not name the pixels it counted: %v", err)
	}
	if _, err := ProcessImage(bytes.NewReader(frame), 1000); !errors.Is(err, ErrTooManyPixels) {
		t.Errorf("the same frame under a ceiling of 1000 pixels: %v", err)
	}
}

// webpFixture is one lossless WebP frame, kept as the bytes it is: x/image decodes
// WebP and does not encode it, so a fixture that was generated here would be a
// fixture that proves the generator works. These are the 442 bytes of a 75x100
// 1-bit-per-pixel WebP from golang.org/x/image's own testdata (BSD-3-Clause, the
// Go Authors), kept because the one thing it has to prove is that a real decoder
// reads a real file.
func webpFixture(t *testing.T) []byte {
	t.Helper()
	const b64 = "UklGRrIBAABXRUJQVlA4TKUBAAAvSsAYAA8w//M///MfeJAkbXvaSG7m8Q3GfYSBJekwQztm/IcZlgwnmWImn2BK7aFmBtnVir6q//8VOkFE/xm4baTIu8c48ArEo6+B3zFKYln3pqClSCKX0begFTAXFOLXHSyF8cCNcZEG4OywuA4KVVfJCiArU7GAgJI8+lJP/OKMT/fBAjevg1cYB7YVkFuWga2lyPi5I0HFy5YTpWIHg0RZpkniRVW9odHAKOwosWuOGdxIyn2OvaCDvhg/we6TwadPBPbqBV58MsLmMJ8yZnOWk8SRz4N+QoyPL+MnamzMvcE1rHNEr91F9GKZPVUcS9w7PhhH36suB9qPeYb/oLk6cuTiJ0wOK3m5h1cKjW6EVZCYMK7dxcKCBdgP9HkKr9gkAO2P8GKZGWVdIAatQa+1IDpt6qyorVwdy01xdW8Jkfk6xjEXmVQQ+HQdFr6OKhIN34dXWq0+0qr6EJSCeeVLH9+gvGTLyqM65PQ44ihzlTXxQKjKbAvshXgir7Lil9w4L2bvMycmjQcqXaMCO6BlY28i+FOLzbfI1vEqxAhotocAAA=="
	b, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatalf("the fixture is not the bytes it claims: %v", err)
	}
	if cfg, format, err := image.DecodeConfig(bytes.NewReader(b)); err != nil || format != "webp" {
		t.Fatalf("the fixture is not a WebP: %dx%d %s %v", cfg.Width, cfg.Height, format, err)
	}
	return b
}
