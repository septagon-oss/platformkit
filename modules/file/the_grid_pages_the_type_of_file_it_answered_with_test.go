package file_test

import (
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

// TestTheGridPagesTheTypeOfFileItAnsweredWith asks the library page to show documents
// only — in a library that also holds pictures whose names the same search matches —
// and then to page that filter. The search, the type and the order are carried into the
// grid's links by the one place that turns a query into a query string, so a link that
// drops one of them asks the screenful behind it a narrower question than the person
// asked: the second screenful of a documents-only search would hand back the pictures
// their own filter had excluded, under a count that still says the filtered total.
// Twenty-one documents and three pictures carry one name fragment, the pictures filed
// first so they are the oldest and the first tiles an unfiltered second screenful draws.
// The address is read out of the anchor the grid draws rather than typed, and a full
// first screenful of documents is the reachability guard: it stands whether or not the
// link keeps the filter.
func TestTheGridPagesTheTypeOfFileItAnsweredWith(t *testing.T) {
	router, _, _, _, _ := libraryFixture(t)
	frame := wideFrame(t)
	pictures := make([]string, 0, 3)
	for i := range 3 {
		name := fmt.Sprintf("paged-kind-image-%d.jpg", i)
		if code, out := upload(t, router, files, name, "image/jpeg", string(frame)); code != http.StatusCreated {
			t.Fatalf("filing %s beside the documents = %d %s", name, code, out)
		}
		pictures = append(pictures, name)
	}
	documents := make([]string, 0, contracts.DefaultLibraryPageSize+1)
	for i := range contracts.DefaultLibraryPageSize + 1 {
		name := fmt.Sprintf("paged-kind-%02d.txt", i)
		if code, out := upload(t, router, files, name, "text/plain", "one report"); code != http.StatusCreated {
			t.Fatalf("filing %s for the filtered grid = %d %s", name, code, out)
		}
		documents = append(documents, name)
	}

	open := url.Values{"q": {"paged-kind"}, "type": {contracts.FamilyDocument}}
	first := libraryPage(t, router, libraryAddress+"?"+open.Encode())
	if onFirst := named(documents, first); len(onFirst) != contracts.DefaultLibraryPageSize {
		t.Fatalf("the library's first screenful of documents drew %d of the %d documents the filter matched, want one full screenful of %d",
			len(onFirst), len(documents), contracts.DefaultLibraryPageSize)
	}
	if leaked := named(pictures, first); len(leaked) != 0 {
		t.Fatalf("a documents-only search drew the pictures its filter excludes: %v", leaked)
	}

	anchor := nextPageLink.FindString(first)
	if anchor == "" {
		t.Fatalf("a filtered search of %d documents offered no link to its second screenful, so the document behind it stays unreachable",
			len(documents))
	}
	found := orderHref.FindStringSubmatch(anchor)
	if len(found) != 2 {
		t.Fatalf("the library's second-screenful link %q carries no address to follow", anchor)
	}
	next := html.UnescapeString(found[1])
	if !strings.Contains(next, "type="+contracts.FamilyDocument) {
		t.Errorf("the library's next-page link %q drops the type of file it was asked for, so the screenful behind it is the whole library", next)
	}

	// The documents are named in the order they were filed and the page lists newest
	// first, so the oldest document is the one tile the first screenful left behind.
	want := documents[0]
	behind := libraryPage(t, router, next)
	if tiles := named(documents, behind); len(tiles) != 1 || tiles[0] != want {
		t.Errorf("the library's second screenful of a documents-only search drew %v, want only %s, the document the first screenful left behind",
			tiles, want)
	}
	if excluded := named(pictures, behind); len(excluded) != 0 {
		t.Errorf("the second screenful of a documents-only search drew the pictures the filter excludes: %v", excluded)
	}
}
