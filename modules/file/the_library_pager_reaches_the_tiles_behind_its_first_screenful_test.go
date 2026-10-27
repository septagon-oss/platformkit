package file_test

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

// TestTheLibraryPagerReachesTheTilesBehindItsFirstScreenful asks the library page the
// question a person with more pictures than one screenful asks it: how many are
// there, and can I get to the ones I cannot see. The page derives its controls from
// the count the search answers with, so a count taken from the rows of one screenful
// draws no page 2 at all — the person is shown the first twenty of their own files
// and given no way to the twenty-first.
//
// One file more than contracts.DefaultLibraryPageSize is filed, all of them reachable
// by name, and the page is read twice: the tiles it draws, the number it prints above
// them and the links it offers must agree across both screenfuls. Each tile is
// reached exactly once, the whole match is the number printed beside every tile, and
// no link promises a third screenful the library cannot fill.
func TestTheLibraryPagerReachesTheTilesBehindItsFirstScreenful(t *testing.T) {
	router, _, _, _, _ := libraryFixture(t)
	filled := contracts.DefaultLibraryPageSize + 1
	names := make([]string, 0, filled)
	for i := range filled {
		name := fmt.Sprintf("paged-%02d.txt", i)
		code, out := upload(t, router, files, name, "text/plain", "one picture sheet")
		if code != http.StatusCreated {
			t.Fatalf("filing %s for the grid = %d %s", name, code, out)
		}
		names = append(names, name)
	}

	first := libraryPage(t, router, libraryAddress)
	onFirst := named(names, first)
	if len(onFirst) != contracts.DefaultLibraryPageSize {
		t.Errorf("the first screenful drew %d of the %d tiles filed, want a full screenful of %d",
			len(onFirst), filled, contracts.DefaultLibraryPageSize)
	}
	if !strings.Contains(first, " of "+strconv.Itoa(filled)) {
		t.Errorf("the grid numbered its tiles against a screenful, want the whole count of %d printed beside them", filled)
	}
	if !strings.Contains(first, `data-page="2"`) {
		t.Errorf("a library of %d files drew no link to its second page, so the tiles behind the first screenful are unreachable", filled)
	}

	second := libraryPage(t, router, libraryAddress+"?page=2")
	if strings.Contains(second, `data-page="3"`) {
		t.Errorf("the library of %d files promised a third page it has no tiles for", filled)
	}
	behind := 0
	for _, name := range names {
		switch {
		case strings.Contains(second, name) && strings.Contains(first, name):
			t.Errorf("the tile %q is drawn on both screenfuls, so paging shows the same file twice", name)
		case strings.Contains(second, name):
			behind++
		}
	}
	if behind != filled-len(onFirst) {
		t.Errorf("the second screenful reached %d of the %d tiles the first did not draw, want every tile behind it",
			behind, filled-len(onFirst))
	}
}

// libraryPage reads one address of the library as a signed member's browser would and
// answers with its markup, failing the case on any answer that is not the page.
func libraryPage(t *testing.T, router http.Handler, at string) string {
	t.Helper()
	rec := libraryRequest(t, router, http.MethodGet, at, host, "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, want the library page", at, rec.Code)
	}
	return rec.Body.String()
}

// named answers with the filed names this markup draws. A name is drawn once per tile
// and nowhere else on a grid with no filter set, so the answer is the tile count.
func named(names []string, markup string) []string {
	out := make([]string, 0, len(names))
	for _, name := range names {
		if strings.Contains(markup, name) {
			out = append(out, name)
		}
	}
	return out
}
