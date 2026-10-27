package file_test

import (
	"net/http"
	"strings"
	"testing"
)

// TestTheGridNumbersItsTilesAgainstTheFilterItAnswers asks the grid what it numbers its
// tiles against once a person has typed part of a name: the whole library, or the match
// it is answering with. The count rides the same filtered read as the rows, so a grid
// that found one of two files numbers its one tile against one and offers no second page
// to a search with one answer. A count taken before the filter is applied prints "1 of 2"
// above that single tile and links a page the filter cannot fill.
func TestTheGridNumbersItsTilesAgainstTheFilterItAnswers(t *testing.T) {
	router, _, _, _, _ := libraryFixture(t)
	for _, name := range []string{"matched-file.txt", "unrelated-item.txt"} {
		if code, out := upload(t, router, files, name, "text/plain", "one picture sheet"); code != http.StatusCreated {
			t.Fatalf("filing %s for the filter = %d %s", name, code, out)
		}
	}

	// The same library read without a filter: both tiles, numbered against both. The
	// filtered answer below is only an answer about the filter if this says two.
	if whole := libraryPage(t, router, libraryAddress); !strings.Contains(whole, "— file 2 of 2") {
		t.Fatalf("the unfiltered library did not number its last tile against the two files this case filed")
	}

	filtered := libraryPage(t, router, libraryAddress+"?q=matched")
	if !strings.Contains(filtered, "matched-file.txt") || strings.Contains(filtered, "unrelated-item.txt") {
		t.Fatalf("the filter q=matched did not draw exactly the one file it matches")
	}
	if !strings.Contains(filtered, "— file 1 of 1") {
		t.Errorf("the grid drew one of two files but numbered that tile against a larger match, want the one match it answers with")
	}
	if strings.Contains(filtered, `data-page="2"`) {
		t.Errorf("the grid offered a link to a second page to a filter whose whole match is on the first")
	}
}
