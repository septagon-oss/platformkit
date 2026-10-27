package file_test

import (
	"html"
	"net/http"
	"net/url"
	"testing"
)

// TestTheLibraryOffersItsWayOutBesideAStampNoRowCarries asks the library for the
// fourth of its filters, the use ledger's stamp, with nothing in the name box, no
// type chosen and no order asked, and asks what that page offers as the way out. The
// control answers the whole query the grid is showing, so what it clears is the stamp
// as much as a name, and this is the one filter of the four no page case reached
// through: stamping a row needs the release sweep, so the grid here is the empty one —
// which is also the grid hardest to get out of, because a person who filtered to what
// nothing shows is looking at no tile to click back from and at no other route to the
// whole library. The probe is that empty grid itself: the rows this case files are on
// the bare page and off the filtered one, which shows the stamp filter stands without
// asking the control anything. The way out is then read out of the filtered page, its
// address asserted, and followed, so a control that answers only the filters that
// narrow the grid, or a link that keeps the stamp it exists to drop, is refused.
func TestTheLibraryOffersItsWayOutBesideAStampNoRowCarries(t *testing.T) {
	router, _, _, _, _ := libraryFixture(t)
	filed := []string{"stampedview-one.txt", "stampedview-two.txt", "stampedview-three.txt"}
	for _, name := range filed {
		if code, out := upload(t, router, files, name, "text/plain", "one report"); code != http.StatusCreated {
			t.Fatalf("filing %s for the library's stamp filter = %d %s", name, code, out)
		}
	}

	filtered := libraryPage(t, router, libraryAddress+"?"+url.Values{"unused": {"1"}}.Encode())
	if onGrid := named(filed, filtered); len(onGrid) != 0 {
		t.Fatalf("the grid asked for only what the use ledger stamped drew %v of the %d rows no sweep has stamped, so this case cannot tell a standing stamp filter from an unfiltered library",
			onGrid, len(filed))
	}
	if whole := named(filed, libraryPage(t, router, libraryAddress)); len(whole) != len(filed) {
		t.Fatalf("the whole library drew %v of the %d rows this case filed, so the rows are not on the screen the way out is meant to reach",
			whole, len(filed))
	}

	way := ghostLink.FindStringSubmatch(filtered)
	if len(way) != 2 {
		t.Fatalf("a grid narrowed by the use ledger's stamp alone offered no way out, though clearing it clears the stamp as much as any name")
	}
	if got := html.UnescapeString(way[1]); got != libraryAddress {
		t.Errorf("the way out offered beside a stamp filter is %q, want the whole library at %q", got, libraryAddress)
	}
	if shown := named(filed, libraryPage(t, router, html.UnescapeString(way[1]))); len(shown) != len(filed) {
		t.Errorf("following the way out of the stamp filter drew %v of the %d rows the library holds, so the filter outlived the control that clears it",
			shown, len(filed))
	}
}
