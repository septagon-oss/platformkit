package file_test

import (
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

// ghostLink is the library's way out of a filter: a link-button that appears only
// while the grid is filtered. It is read by what it is (a link styled as the page's
// secondary action) rather than by its sentence, which the request's language owns.
var ghostLink = regexp.MustCompile(`<a [^>]*data-variant="ghost"[^>]*href="([^"]*)"`)

// TestTheLibraryLinksBackToTheWholeLibraryFromBehindItsFilters asks for documents
// only in a library that also holds pictures the same search matches, then follows
// the page's own way out of the filter and asks what the grid shows afterwards.
// A clear that submits the fields it sits beside, or a link that carries the query it
// is meant to drop, leaves the person on the filtered grid they clicked away from, so
// the screenful behind that control is asserted, not just its address. Three pictures
// and five documents carry one name fragment — fewer than one page holds, so the
// whole library fits on the answer and only the filter can hide a name.
func TestTheLibraryLinksBackToTheWholeLibraryFromBehindItsFilters(t *testing.T) {
	router, _, _, _, _ := libraryFixture(t)
	frame := wideFrame(t)
	pictures := make([]string, 0, 3)
	for i := range 3 {
		name := fmt.Sprintf("clear-kind-image-%d.jpg", i)
		if code, out := upload(t, router, files, name, "image/jpeg", string(frame)); code != http.StatusCreated {
			t.Fatalf("filing %s beside the filter = %d %s", name, code, out)
		}
		pictures = append(pictures, name)
	}
	documents := make([]string, 0, 5)
	for i := range 5 {
		name := fmt.Sprintf("clear-kind-%d.txt", i)
		if code, out := upload(t, router, files, name, "text/plain", "one report"); code != http.StatusCreated {
			t.Fatalf("filing %s for the filter = %d %s", name, code, out)
		}
		documents = append(documents, name)
	}

	open := url.Values{"q": {"clear-kind"}, "type": {contracts.FamilyDocument}}
	filtered := libraryPage(t, router, libraryAddress+"?"+open.Encode())
	// Reachability, standing whichever way the way out goes: the filter is in effect,
	// so a picture is off the screen the control is meant to clear.
	if onGrid := named(documents, filtered); len(onGrid) != len(documents) {
		t.Fatalf("the documents-only search drew %v of the %d documents it matched, so no filter stands to be cleared",
			onGrid, len(documents))
	}
	if leaked := named(pictures, filtered); len(leaked) != 0 {
		t.Fatalf("the documents-only search drew the pictures its filter excludes: %v", leaked)
	}
	found := ghostLink.FindStringSubmatch(filtered)
	if len(found) != 2 {
		t.Fatalf("a filtered grid offered no secondary link out of the filter, so the person asking for the whole library has no control that answers")
	}
	out := html.UnescapeString(found[1])
	if strings.ContainsAny(strings.TrimPrefix(out, libraryAddress), "?&") {
		t.Errorf("the library's way out of its filters is %q, which asks the whole library again with the filter still on it", out)
	}
	back := libraryPage(t, router, out)
	if hidden := named(pictures, back); len(hidden) != len(pictures) {
		t.Errorf("following the library's way out of its filters %q left %v of the %d pictures off the grid, so the filter outlived the control that clears it",
			out, hidden, len(pictures))
	}
	if gone := named(documents, back); len(gone) != len(documents) {
		t.Errorf("following the library's way out of its filters %q drew %v of the %d documents, so the grid behind it is not the whole library",
			out, gone, len(documents))
	}
}
