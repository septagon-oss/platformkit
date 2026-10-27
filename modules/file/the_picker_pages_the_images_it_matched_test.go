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

// pickerPageLink is the anchor the picker's pager offers for a numbered page, href and
// all. The case follows that href rather than typing an address of its own: it is the
// address a click on the picker travels to, and it has to carry the search, the calling
// field and the caller's return path with it, because the picker answers the page that
// opened it and nothing else.
var pickerPageLink = regexp.MustCompile(`(?s)<a [^>]*data-page="2"[^>]*>`)

var pickerHref = regexp.MustCompile(`href="([^"]+)"`)

// TestThePickerPagesTheImagesItMatched asks the picker what the library grid's pager is
// asked at one picture more than it draws. The picker is a second derivation of the same
// figure — its own count, its own page links, its own address — on the pick route, and a
// person paging there is choosing between pictures rather than browsing them. Twenty-one
// images carry one name fragment and one image does not, so the search is 21 pictures over
// two screenfuls while the library holds 22. The case reads the next-page address out of
// the markup and opens it: a link that drops the search answers a question nobody asked,
// and a link that drops the field or the return path lands the person somewhere that
// cannot hand the choice back to the page that opened the picker. The tiles drawn on the
// first screenful, and the existence of that link, are the reachability guards: they pass
// under either defect.
func TestThePickerPagesTheImagesItMatched(t *testing.T) {
	router, _, _, _, _ := libraryFixture(t)
	frame := wideFrame(t)
	pictures := make([]string, 0, contracts.DefaultLibraryPageSize+2)
	for i := range contracts.DefaultLibraryPageSize + 1 {
		code, out := upload(t, router, files, fmt.Sprintf("picked-fragment-%02d.jpg", i), "image/jpeg", string(frame))
		if code != http.StatusCreated {
			t.Fatalf("filing picked-fragment-%02d.jpg for the picker = %d %s", i, code, out)
		}
		pictures = append(pictures, field(t, out, "id"))
	}
	code, out := upload(t, router, files, "unpicked-item.jpg", "image/jpeg", string(frame))
	if code != http.StatusCreated {
		t.Fatalf("filing the image the fragment does not match = %d %s", code, out)
	}
	excluded := field(t, out, "id")

	open := url.Values{"returns": {"/app/content/contents/new"}, "field": {"image"}, "q": {"picked-fragment"}}
	first := libraryPage(t, router, libraryAddress+"/pick?"+open.Encode())
	onFirst := reached(pictures, first)
	if len(onFirst) != contracts.DefaultLibraryPageSize {
		t.Fatalf("the picker's first screenful drew %d of the %d pictures the search matched, want one full screenful of %d",
			len(onFirst), len(pictures), contracts.DefaultLibraryPageSize)
	}
	if strings.Contains(first, excluded) {
		t.Errorf("the picker's first screenful drew the image the search excluded, so its count is not the search's")
	}
	anchor := pickerPageLink.FindString(first)
	if anchor == "" {
		t.Fatalf("a search of %d pictures offered no link to its second screenful, so the picture behind it cannot be chosen",
			len(pictures))
	}
	stamp := pickerHref.FindStringSubmatch(anchor)
	if len(stamp) != 2 {
		t.Fatalf("the picker's second-screenful link %q carries no address to follow", anchor)
	}
	// The markup escapes an ampersand the way HTML asks; a browser follows the address
	// the entity stands for, so the case opens that address too.
	next := html.UnescapeString(stamp[1])
	for _, kept := range []string{"q=picked-fragment", "field=image", "returns=%2Fapp%2Fcontent%2Fcontents%2Fnew"} {
		if !strings.Contains(next, kept) {
			t.Errorf("the picker's next-page link %q drops %s, so the click leaves the picker somewhere else", next, kept)
		}
	}

	second := libraryPage(t, router, next)
	if behind := reached(pictures, second); len(behind) != 1 || strings.Contains(first, behind[0]) {
		t.Fatalf("the picker's second screenful drew %v, want the one picture the first screenful left behind", behind)
	}
	if strings.Contains(second, `data-page="3"`) {
		t.Errorf("a search of %d pictures offered a third page it has no pictures for", len(pictures))
	}
}

// reached answers with the filed ids this markup draws. A row is drawn once per tile, so
// the answer is the tile count read out of the rows the picker actually has.
func reached(ids []string, markup string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if strings.Contains(markup, id) {
			out = append(out, id)
		}
	}
	return out
}
