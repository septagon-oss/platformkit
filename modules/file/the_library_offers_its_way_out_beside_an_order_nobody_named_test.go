package file_test

import (
	"html"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

// TestTheLibraryOffersItsWayOutBesideAnOrderNobodyNamed asks the whole library — every
// row it holds, no name typed and no type chosen — first in name order and then in the
// reverse of name order, and asks what each of those pages offers as the way back. The
// order is the third filter the grid's query carries: the way out answers that query, so
// what it clears is the order as much as a name or a type, and a control kept for the two
// filters that narrow the grid would leave the person who only re-ordered it looking at a
// page that offers them nothing to go back on. The probe is the tile order the page itself
// drew, which is opposite on the two answers and says nothing about the control, so a
// standing order filter is shown before the way out is looked for. The address that way
// out carries is then read out of the page and followed, so a link that keeps the order it
// is meant to drop is refused by the grid behind it and not only by its own query. Nothing
// here is counted against the tree: the library is the three rows this case files.
func TestTheLibraryOffersItsWayOutBesideAnOrderNobodyNamed(t *testing.T) {
	router, _, _, _, _ := libraryFixture(t)
	// Filed in an order that is neither name order nor its reverse, and named so that none
	// of the three is a substring of another: the only reading of the grid is its tile order.
	filed := []string{"sortview-mid.txt", "sortview-alpha.txt", "sortview-zeta.txt"}
	for _, name := range filed {
		if code, out := upload(t, router, files, name, "text/plain", "one report"); code != http.StatusCreated {
			t.Fatalf("filing %s for the library's order = %d %s", name, code, out)
		}
	}

	byName := []string{"sortview-alpha.txt", "sortview-mid.txt", "sortview-zeta.txt"}
	opposite := []string{byName[2], byName[1], byName[0]}
	ascending := libraryPage(t, router, libraryAddress+"?"+url.Values{"sort": {contracts.SortName}}.Encode())
	descending := libraryPage(t, router, libraryAddress+"?"+url.Values{"sort": {contracts.SortNameDesc}}.Encode())
	if !drawnIn(ascending, byName) {
		t.Fatalf("the grid asked for in name order did not draw %v in that order, so this case cannot tell an asked order from the default", byName)
	}
	if !drawnIn(descending, opposite) {
		t.Fatalf("the grid asked for in the reverse of name order did not draw %v in that order, so this case cannot tell an asked order from the default", opposite)
	}

	way := ghostLink.FindStringSubmatch(descending)
	if len(way) != 2 {
		t.Fatalf("a grid set to one order with no name and no type offered no way out, though clearing it clears the order as much as any name")
	}
	if got := html.UnescapeString(way[1]); got != libraryAddress {
		t.Errorf("the way out offered beside an order filter is %q, want the whole library at %q", got, libraryAddress)
	}
	if shown := named(filed, libraryPage(t, router, html.UnescapeString(way[1]))); len(shown) != len(filed) {
		t.Errorf("following the way out of the order filter drew %v of the %d rows the library holds, so the filter outlived the control that clears it",
			shown, len(filed))
	}
}

// drawnIn answers whether this markup draws exactly these names, once each, in this
// order: the tile order the page chose, read out of the markup it returned.
func drawnIn(markup string, order []string) bool {
	seen := -1
	for _, name := range order {
		at := strings.Index(markup, name)
		if at <= seen {
			return false
		}
		seen = at
	}
	return true
}
