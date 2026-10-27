package file_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

// TestTheGridKeepsAFrameNothingMeasuredOutOfItsPictures asks the family filters about a
// file whose media type this deployment would render inline but whose frame nothing ever
// measured. Being a picture is a pair of clauses — a renderable image type, and a width
// the image pass actually read — and the second is what separates "this is a photograph"
// from "somebody typed photograph": the door lets a declared image that no decoder reads
// through as it arrived, so the row has no width, and contracts.MediaFamily calls such a
// row a document. The grid's SQL is the same decision written over the column, and the
// two answers had no case reaching them: the row that separates them is excluded from
// images and owed a tile among documents by both sides of that decision at once. Three
// files carry one name fragment: a frame the pass measured, a declared image no decoder
// reads, and a plain text note beside them.
func TestTheGridKeepsAFrameNothingMeasuredOutOfItsPictures(t *testing.T) {
	router, _, _, _, _ := libraryFixture(t)
	frame := wideFrame(t)
	filed := make([]string, 0, 3)
	measured := "measured-picture-frame.jpg"
	if code, out := upload(t, router, files, measured, "image/jpeg", string(frame)); code != http.StatusCreated {
		t.Fatalf("filing the measured frame = %d %s", code, out)
	}
	filed = append(filed, measured)
	// No decoder reads AVIF here and the sniffer has no opinion about these bytes, so the
	// door keeps the upload as it came: a declared image with no width behind it.
	unmeasured := "measured-picture-avif.avif"
	if code, out := upload(t, router, files, unmeasured, "image/avif",
		"\x00\x00\x00\x20ftypavif\x00\x00\x00\x00avifmif1mia1"); code != http.StatusCreated {
		t.Fatalf("filing the image no decoder read = %d %s", code, out)
	}
	filed = append(filed, unmeasured)
	note := "measured-picture-note.txt"
	if code, out := upload(t, router, files, note, "text/plain", "a note beside the pictures"); code != http.StatusCreated {
		t.Fatalf("filing the note beside the pictures = %d %s", code, out)
	}
	filed = append(filed, note)

	// The reachability guard, which a family filter cannot change: all three carry the
	// fragment, so the unfiltered library draws three tiles.
	whole := libraryPage(t, router, libraryAddress+"?q=measured-picture")
	if drew := named(filed, whole); len(drew) != len(filed) {
		t.Fatalf("the library named by %q drew %d of the %d files this case filed", "measured-picture", len(drew), len(filed))
	}

	pictures := libraryPage(t, router, libraryAddress+"?"+url.Values{
		"q": {"measured-picture"}, "type": {contracts.FamilyImage},
	}.Encode())
	if drew := named([]string{measured}, pictures); len(drew) != 1 {
		t.Errorf("the pictures-only grid did not draw %s, the frame the image pass measured", measured)
	}
	if drew := named([]string{unmeasured}, pictures); len(drew) != 0 {
		t.Errorf("the pictures-only grid drew %s, whose frame nothing measured — a tile it cannot draw, "+
			"against the answer contracts.MediaFamily gives: %v", unmeasured, drew)
	}

	documents := libraryPage(t, router, libraryAddress+"?"+url.Values{
		"q": {"measured-picture"}, "type": {contracts.FamilyDocument},
	}.Encode())
	if drew := named([]string{unmeasured, note}, documents); len(drew) != 2 {
		t.Errorf("the documents-only grid drew %v of the 2 files nobody measured a frame of, want both %s and %s",
			len(drew), unmeasured, note)
	}
	if drew := named([]string{measured}, documents); len(drew) != 0 {
		t.Errorf("the documents-only grid drew %s, the frame the image pass measured: %v", measured, drew)
	}
}
