package file_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

// TestTheLibraryHoldsEveryFileItMatchedInOneFamilyOnly pins what every family
// filter rests on: the three filters partition the match set. A row the library
// lists is a picture, a video or a document — never two of them, because the type
// filter is a radio control and a row in two families is drawn twice under one
// choice, and never none of them, because a row in none vanishes from a library
// that counted it.
//
// The answer is kept twice: contracts.MediaFamily reads the Go value and the grid's
// SQL reads the column, through an expression written to match it. So the rows
// here are filed by media type rather than by size. An image row carries the media
// type the image pass decoded, which arrives normalised; a video row and a text row
// carry the string the upload declared, parameters and all, and only the essence
// decides their family. Both therefore declare a type with a space and a parameter
// in front of it: strip anything less than the whole expression and the video lands
// in two families, while strip too little and it lands in none — and a person
// watching the library cannot tell either failure apart from an empty filter.
func TestTheLibraryHoldsEveryFileItMatchedInOneFamilyOnly(t *testing.T) {
	router, _, _, _, _ := libraryFixture(t)
	const filed = "one-family-"
	f := func(name, contentType, body string) string {
		t.Helper()
		code, out := upload(t, router, files, filed+name, contentType, body)
		if code != http.StatusCreated {
			t.Fatalf("filing %s = %d %s", name, code, out)
		}
		return filed + name
	}
	picture := f("frame.jpg", "image/jpeg ;charset=utf-8", string(wideFrame(t)))
	video := f("clip.mp4", "video/mp4 ;charset=utf-8", "\x00\x00\x00\x20ftypisom\x00\x00\x02\x00isomiso2avc1mp41")
	note := f("note.txt", "text/plain ;charset=utf-8", "a note beside the film")
	all := []string{picture, video, note}

	// The reachability guard, which no family filter can move: one search, no
	// type set, every tile this case filed.
	whole := libraryPage(t, router, libraryAddress+"?"+url.Values{"q": {filed}}.Encode())
	if drew := named(all, whole); len(drew) != len(all) {
		t.Fatalf("the library named by %q drew %v of the %d files this case filed", filed, drew, len(all))
	}

	for _, want := range []struct {
		family string
		names  []string
	}{
		{contracts.FamilyImage, []string{picture}},
		{contracts.FamilyVideo, []string{video}},
		{contracts.FamilyDocument, []string{note}},
	} {
		page := libraryPage(t, router, libraryAddress+"?"+url.Values{
			"q": {filed}, "type": {want.family},
		}.Encode())
		drew := named(all, page)
		if len(drew) != len(want.names) || drew[0] != want.names[0] {
			t.Errorf("type=%s drew %v, want only %v: the three filters are meant to partition the match set",
				want.family, drew, want.names)
		}
	}
}
