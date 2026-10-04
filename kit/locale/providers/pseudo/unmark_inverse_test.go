package pseudo_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/locale/providers/pseudo"
)

// Mark's documentation says Unmark "is its exact inverse for every string". The
// reserving pass hands out ạ ȧ ẹ ḭ ọ ṵ (and their capitals) to stand for an accented
// vowel the payload already held — so a payload that held one of those runes in the
// first place, as Vietnamese copy does ("Chào ạ", "Có ọ"), must still read back as
// itself.
func TestUnmarkReadsBackAPayloadThatAlreadyHeldAReservedRune(t *testing.T) {
	for _, payload := range []string{"ạ", "Chào ạ", "Có ọ", "ẹ ḭ ṵ ȧ", "Ạ Ọ Ẹ Ḭ Ṳ Ȧ", "áạ"} {
		marked := pseudo.Mark(payload)
		if !pseudo.Wrapped(marked) {
			t.Fatalf("Mark(%q) = %q, which Wrapped does not recognise", payload, marked)
		}
		if back := pseudo.Unmark(marked); back != payload {
			t.Errorf("Unmark(Mark(%q)) = %q; the map is not a bijection over this payload", payload, back)
		}
	}
}
