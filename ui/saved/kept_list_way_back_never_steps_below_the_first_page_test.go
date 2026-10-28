package saved

import (
	"html"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/ui/page"
)

// TestTheKeptListWayBackNeverStepsBelowTheFirstPage holds the kept list's way back to
// two bounds no case in this repository asks of it: the address offered for the newer
// copies stops at the first copy rather than spelling an offset below zero, and it is a
// page's distance back rather than the distance the answer happened to carry. A person
// may reach this page at an offset they typed themselves, and one step back from inside a
// page is then not a whole page — the figure has to be clamped to remain a page, since an
// offset below zero is no page anybody can answer and a step drawn from the rows answered
// walks a person back over copies they have just read.
//
// One filtered pair is asked at a page size of four, three times: at the second page,
// where a whole page back is the first copy; five copies into the third page with a short
// answer, where the two figures part company; and one copy in, which only a clamped step
// answers with the first copy. The way on is read beside the way back, because both links
// come out of one request and one answer and a bound kept on one alone is not a bound.
func TestTheKeptListWayBackNeverStepsBelowTheFirstPage(t *testing.T) {
	at := At{List: "/app/keeping/saved", Save: "/app/keeping/saved/save", Remove: "/app/keeping/saved"}
	const (
		pair    = "module=look&kind=outfit"
		perPage = 4
	)
	// Only the pager writes an address with a query on it: a card links to the record it
	// is a copy of, its give-back posts to a form action, and the breadcrumb's link to the
	// workspace carries nothing.
	offered := func(view page.View) []string {
		var out []string
		for _, n := range view.Body {
			var drawn strings.Builder
			if err := n.Render(&drawn); err != nil {
				t.Fatalf("the kept list could not be rendered: %v", err)
			}
			for _, href := range regexp.MustCompile(`href="([^"]*\?[^"]*)"`).FindAllStringSubmatch(drawn.String(), -1) {
				out = append(out, html.UnescapeString(href[1]))
			}
		}
		return out
	}
	for _, ask := range []struct {
		name   string
		offset int
		rows   int
		want   []string
	}{
		{name: "the second page, asked where a page begins", offset: perPage, rows: perPage,
			want: []string{at.List + "?" + pair + "&offset=0", at.List + "?" + pair + "&offset=8"}},
		{name: "five copies into the third page, answered short", offset: 9, rows: 2,
			want: []string{at.List + "?" + pair + "&offset=5", at.List + "?" + pair + "&offset=13"}},
		{name: "one copy in, a number nobody clicked", offset: 1, rows: perPage,
			want: []string{at.List + "?" + pair + "&offset=0", at.List + "?" + pair + "&offset=5"}},
	} {
		copies := Page{Total: 40}
		for i := 0; i < ask.rows; i++ {
			copies.Rows = append(copies.Rows, Row{
				ID: "5b1f1e07-9a44-4f2b-9f2a-2a2f0f0d3f4c", Module: "look", Kind: "outfit", Key: "one",
				Title: "A kept copy", Captured: time.Unix(1700000000, 0).UTC(),
				Availability: AvailabilityAvailable, SourceURL: "/app/look/outfits/one", Revision: 3,
			})
		}
		view := grid(page.Request{}, page.Shell{BackLabel: "Workspace"}, Options{Workspace: "/app"},
			at, copies, filters{Module: "look", Kind: "outfit", Offset: ask.offset}, perPage)
		if view.Status != http.StatusOK {
			t.Errorf("%s: the kept list answered %d, not the page of copies it draws", ask.name, view.Status)
			continue
		}
		if got := offered(view); !slices.Equal(got, ask.want) {
			t.Errorf("%s: the kept list offered %v, want %v", ask.name, got, ask.want)
		}
	}
}
