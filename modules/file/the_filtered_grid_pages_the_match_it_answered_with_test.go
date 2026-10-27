package file_test

import (
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

// nextPageLink is the anchor the grid's pager offers for page 2, href and all: the address
// a click actually travels to when a person asks a search for its next screenful, rather
// than one the case typed for itself.
var nextPageLink = regexp.MustCompile(`<a [^>]*data-page="2"[^>]*>`)

// TestTheFilteredGridPagesTheMatchItAnsweredWith asks the library page what a person with
// more matching pictures than one screenful asks it: whether the number beside a tile, the
// link to the next screenful, and the tiles that link shows all answer to the search they
// typed. Twenty-one files carry one name fragment and one file does not, so the search is
// 21 matches over two screenfuls while the library holds 22. A total taken before the
// filter numbers the search against the files it excluded; a next-page link that drops the
// filter shows the whole library to someone who asked for part of it; and a count taken
// from one screenful's rows offers no link at all, hiding the match that came next.
func TestTheFilteredGridPagesTheMatchItAnsweredWith(t *testing.T) {
	router, _, _, _, _ := libraryFixture(t)
	matches := contracts.DefaultLibraryPageSize + 1
	filed := make([]string, 0, matches+1)
	for i := range matches {
		name := fmt.Sprintf("paged-fragment-%02d.txt", i)
		if code, out := upload(t, router, files, name, "text/plain", "one picture sheet"); code != http.StatusCreated {
			t.Fatalf("filing %s for the filtered grid = %d %s", name, code, out)
		}
		filed = append(filed, name)
	}
	if code, out := upload(t, router, files, "unrelated-item.txt", "text/plain", "one picture sheet"); code != http.StatusCreated {
		t.Fatalf("filing the file the fragment does not match = %d %s", code, out)
	}

	first := libraryPage(t, router, libraryAddress+"?q=paged-fragment")
	if onFirst := named(filed, first); len(onFirst) != contracts.DefaultLibraryPageSize {
		t.Fatalf("the filtered first screenful drew %d tiles, want one full screenful of %d of the %d matches",
			len(onFirst), contracts.DefaultLibraryPageSize, matches)
	}
	if !strings.Contains(first, "of "+strconv.Itoa(matches)) {
		t.Errorf("the filtered grid numbered its tiles against a total other than the %d matches it answered with", matches)
	}
	link := nextPageLink.FindString(first)
	if link == "" {
		t.Fatalf("a search of %d matches offered no link to its second screenful, so the match behind it is unreachable", matches)
	}
	if !strings.Contains(link, "q=paged-fragment") {
		t.Errorf("the search's next-page link %q drops the filter, so the click answers a question nobody asked", link)
	}

	behind := libraryPage(t, router, libraryAddress+"?q=paged-fragment&page=2")
	if tiles := named(filed, behind); len(tiles) != 1 || strings.Contains(first, tiles[0]) {
		t.Fatalf("the second screenful of the search drew %v, want the one match the first left behind and not one it showed",
			tiles)
	}
	if !strings.Contains(behind, "of "+strconv.Itoa(matches)) {
		t.Errorf("the second screenful numbered its tile against a total other than the %d matches the search found", matches)
	}
	if strings.Contains(behind, `data-page="3"`) {
		t.Errorf("a search of %d matches offered a third page it has no tiles for", matches)
	}
}
