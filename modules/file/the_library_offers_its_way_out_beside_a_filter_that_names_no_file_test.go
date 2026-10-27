package file_test

import (
	"html"
	"net/http"
	"net/url"
	"testing"

	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

// TestTheLibraryOffersItsWayOutBesideAFilterThatNamesNoFile asks the library for one
// type of file with nothing in the name box, and asks what that page offers as the way
// out. The control answers the filter the grid is showing rather than the name box: what
// it clears is every filter the answer carries — the type, the unused stamp and the order
// as well as a name — so a person who narrowed the grid by type alone has to be able to
// get out of it. A control kept for a typed name only leaves that grid as the only screen
// they can get back from, and the library offers no other way to the whole of it. The
// probe is the grid the filter drew, not the control: the picture the type excludes is off
// that screen and the document it keeps is on it, which shows a filter stands here.
func TestTheLibraryOffersItsWayOutBesideAFilterThatNamesNoFile(t *testing.T) {
	router, _, _, _, _ := libraryFixture(t)
	picture := "type-only-picture.jpg"
	if code, out := upload(t, router, files, picture, "image/jpeg", string(wideFrame(t))); code != http.StatusCreated {
		t.Fatalf("filing %s beside the type filter = %d %s", picture, code, out)
	}
	document := "type-only-document.txt"
	if code, out := upload(t, router, files, document, "text/plain", "one report"); code != http.StatusCreated {
		t.Fatalf("filing %s for the type filter = %d %s", document, code, out)
	}

	filtered := libraryPage(t, router, libraryAddress+"?"+url.Values{
		"type": {contracts.FamilyDocument},
	}.Encode())
	if onGrid := named([]string{document}, filtered); len(onGrid) != 1 {
		t.Fatalf("the documents-only grid left %s off the screen, so this case cannot tell a standing filter from an empty library", document)
	}
	if leaked := named([]string{picture}, filtered); len(leaked) != 0 {
		t.Fatalf("the documents-only grid drew the picture its type filter excludes: %v", leaked)
	}

	opened := ghostLink.FindStringSubmatch(filtered)
	if len(opened) != 2 {
		t.Fatalf("a grid narrowed by type alone offered no way out of the filter it is drawing, though clearing it clears the type as much as any name")
	}
	if got := html.UnescapeString(opened[1]); got != libraryAddress {
		t.Errorf("the way out offered beside a type filter is %q, want the whole library at %q", got, libraryAddress)
	}
}
