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

// TestTheLibraryPagerLinksTheNextScreenfulInTheOrderItWasAskedFor asks the library page,
// not the picker, to list a search in name order instead of the newest-first order it
// defaults to, and then to page it. The grid's own links — its pager, its clear button,
// its tiles — are built once by filters, so the order survives in them or leaves all of
// them together: a pager that drops it asks the screenful behind it a different question
// than the person asked, and they meet a file they have already seen while the one behind
// it stays hidden. Twenty-one files carry one name fragment, named in the order they were
// filed so that name order and the newest-first default run opposite ways and the newest
// file is last in name order. The case reads the second screenful's address out of the
// anchor the grid draws rather than typing one of its own, then opens it. A full first
// screenful and the existence of that anchor are the reachability guards: they stand
// whichever way the order goes, so the case cannot be green while the page is wrong.
func TestTheLibraryPagerLinksTheNextScreenfulInTheOrderItWasAskedFor(t *testing.T) {
	router, _, _, _, _ := libraryFixture(t)
	matches := contracts.DefaultLibraryPageSize + 1
	filed := make([]string, 0, matches)
	for i := range matches {
		name := fmt.Sprintf("ordered-%02d.txt", i)
		if code, out := upload(t, router, files, name, "text/plain", "one picture sheet"); code != http.StatusCreated {
			t.Fatalf("filing %s for the library's own pager = %d %s", name, code, out)
		}
		filed = append(filed, name)
	}

	open := url.Values{"q": {"ordered"}, "sort": {contracts.SortName}}
	first := libraryPage(t, router, libraryAddress+"?"+open.Encode())
	if onFirst := named(filed, first); len(onFirst) != contracts.DefaultLibraryPageSize {
		t.Fatalf("the library's first screenful in name order drew %d of the %d files the search matched, want one full screenful of %d",
			len(onFirst), matches, contracts.DefaultLibraryPageSize)
	}
	anchor := nextPageLink.FindString(first)
	if anchor == "" {
		t.Fatalf("a search of %d files offered no link to its second screenful, so the file behind it stays unreachable", matches)
	}
	found := orderHref.FindStringSubmatch(anchor)
	if len(found) != 2 {
		t.Fatalf("the library's second-screenful link %q carries no address to follow", anchor)
	}
	next := html.UnescapeString(found[1])
	if !strings.Contains(next, "sort="+contracts.SortName) {
		t.Errorf("the library's next-page link %q drops the order it was asked for, so the screenful behind it is listed by the default", next)
	}

	// The files are named in the order they were filed, so the newest is last in name
	// order: only a second screenful that kept the asked order shows it, and one that fell
	// back to newest-first shows the oldest file, which the first screenful already drew.
	want := filed[len(filed)-1]
	behind := libraryPage(t, router, next)
	if tiles := named(filed, behind); len(tiles) != 1 || tiles[0] != want {
		t.Errorf("the library's second screenful in name order drew %v, want only %s, the newest file, which is last in name order and the one the first screenful left behind",
			tiles, want)
	}
}
