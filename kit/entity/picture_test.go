package entity_test

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
)

func TestComposedPictureReferencesAreCanonicalBoundedSegments(t *testing.T) {
	for _, key := range []string{"source", "a/b ?q=#x", "日本語", strings.Repeat("x", 512)} {
		p, err := entity.ComposedPicture(key)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := entity.PictureKey(p)
		if err != nil || decoded != key {
			t.Fatalf("roundtrip %q: %q %v", key, decoded, err)
		}
	}
	for _, key := range []string{"", strings.Repeat("x", 513), "line\nfeed", "\x00", "\x7f", "\xff"} {
		if p, err := entity.ComposedPicture(key); p != "" || err == nil {
			t.Fatalf("accepted key %q", key)
		}
	}
	for _, p := range []string{"", "c2.eA", "c1.eA==", "c1.eB", " c1.eA", "c1.", "c1." + strings.Repeat("x", 684), "c1." + base64.RawURLEncoding.EncodeToString([]byte("\n"))} {
		if _, err := entity.PictureKey(entity.Picture(p)); err == nil {
			t.Fatalf("accepted token %q", p)
		}
	}
}
