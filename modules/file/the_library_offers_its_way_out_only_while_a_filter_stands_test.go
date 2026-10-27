package file_test

import (
	"html"
	"net/http"
	"net/url"
	"testing"

	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

// TestTheLibraryOffersItsWayOutOnlyWhileAFilterStands asks the library twice — once
// filtered to one type of file, once as asked for — and asks what the page offers as
// the way out in each. The control is the answer to a filter, so it belongs on the
// filtered page only: offered beside no filter it is a link that clears nothing and
// competes with the page's own filled action, and withheld from a filtered grid it
// leaves that grid as the only screen the person can get back from. The first page
// also shows why a type filter is what stands here: a picture the same search matches
// is off it, so the filter has something to be cleared.
func TestTheLibraryOffersItsWayOutOnlyWhileAFilterStands(t *testing.T) {
	router, _, _, _, _ := libraryFixture(t)
	picture := "way-out-picture.jpg"
	if code, out := upload(t, router, files, picture, "image/jpeg", string(wideFrame(t))); code != http.StatusCreated {
		t.Fatalf("filing %s beside the filter = %d %s", picture, code, out)
	}
	document := "way-out-document.txt"
	if code, out := upload(t, router, files, document, "text/plain", "one report"); code != http.StatusCreated {
		t.Fatalf("filing %s for the filter = %d %s", document, code, out)
	}

	filtered := libraryPage(t, router, libraryAddress+"?"+url.Values{
		"q": {"way-out"}, "type": {contracts.FamilyDocument},
	}.Encode())
	if onGrid := named([]string{document}, filtered); len(onGrid) != 1 {
		t.Fatalf("the documents-only search drew %v of the one document it matched, so no filter stands whose way out this case asks about", onGrid)
	}
	if leaked := named([]string{picture}, filtered); len(leaked) != 0 {
		t.Fatalf("the documents-only search drew the picture its filter excludes: %v", leaked)
	}
	opened := ghostLink.FindStringSubmatch(filtered)
	if len(opened) != 2 {
		t.Fatalf("a filtered grid offered no way out of the filter it is drawing")
	}
	if got := html.UnescapeString(opened[1]); got != libraryAddress {
		t.Errorf("the way out offered beside a filter is %q, want the whole library at %q", got, libraryAddress)
	}

	whole := libraryPage(t, router, libraryAddress)
	if shown := named([]string{picture, document}, whole); len(shown) != 2 {
		t.Fatalf("the library asked for with no filter drew %v of the two filed names, so this case cannot read what a filter hides", shown)
	}
	if beside := ghostLink.FindString(whole); beside != "" {
		t.Errorf("the unfiltered library offers %s, which clears nothing: the control belongs beside a filter only", beside)
	}
}
