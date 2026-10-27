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

// orderLink is the anchor the picker's pager offers for a numbered page, href and all,
// and orderHref the address inside it. The case follows that address rather than typing
// one: it is where a click on the picker travels.
var orderLink = regexp.MustCompile(`(?s)<a [^>]*data-page="2"[^>]*>`)

var orderHref = regexp.MustCompile(`href="([^"]+)"`)

// TestThePickerKeepsTheOrderItWasAskedFor asks the picker to list its search in name
// order instead of the newest-first order it defaults to, and then to page it. The
// picker reads an order out of the request and answers the screenful in it, so the
// address it draws for the next screenful has to carry that order too: a link that
// drops it asks the second screenful a different question than the person asked, and
// they meet one picture twice and never see another. Twenty-one pictures carry one name
// fragment, named so that the name order and the newest-first default run opposite
// ways — the newest picture is last in name order — so the second screenful answers
// which order it actually used. The full first screenful and the existence of the link
// are the reachability guards: they stand whichever way the order goes.
func TestThePickerKeepsTheOrderItWasAskedFor(t *testing.T) {
	router, _, _, _, _ := libraryFixture(t)
	frame := wideFrame(t)
	pictures := make([]string, 0, contracts.DefaultLibraryPageSize+1)
	for i := range contracts.DefaultLibraryPageSize + 1 {
		code, out := upload(t, router, files, fmt.Sprintf("ordered-fragment-%02d.jpg", i), "image/jpeg", string(frame))
		if code != http.StatusCreated {
			t.Fatalf("filing ordered-fragment-%02d.jpg = %d %s", i, code, out)
		}
		pictures = append(pictures, field(t, out, "id"))
	}

	open := url.Values{
		"returns": {"/app/content/contents/new"}, "field": {"image"},
		"q": {"ordered-fragment"}, "sort": {contracts.SortName},
	}
	first := libraryPage(t, router, libraryAddress+"/pick?"+open.Encode())
	if onFirst := reached(pictures, first); len(onFirst) != contracts.DefaultLibraryPageSize {
		t.Fatalf("the picker's first screenful in name order drew %d of the %d pictures the search matched, want one full screenful of %d",
			len(onFirst), len(pictures), contracts.DefaultLibraryPageSize)
	}
	anchor := orderLink.FindString(first)
	if anchor == "" {
		t.Fatalf("a search of %d pictures offered no link to its second screenful", len(pictures))
	}
	found := orderHref.FindStringSubmatch(anchor)
	if len(found) != 2 {
		t.Fatalf("the picker's second-screenful link %q carries no address to follow", anchor)
	}
	next := html.UnescapeString(found[1])
	if !strings.Contains(next, "sort="+contracts.SortName) {
		t.Errorf("the picker's next-page link %q drops the order it was asked for, so the second screenful answers a different question", next)
	}

	// The pictures are named in the order they were filed, so the newest is last in
	// name order: only a second screenful that kept the asked order shows it, and one
	// that fell back to newest-first shows the oldest picture, which is already drawn.
	want := pictures[len(pictures)-1]
	second := libraryPage(t, router, next)
	if behind := reached(pictures, second); len(behind) != 1 || behind[0] != want {
		t.Errorf("the picker's second screenful in name order drew %v, want only %s, the newest picture, which is last in name order and the one the first screenful left behind", behind, want)
	}
}
