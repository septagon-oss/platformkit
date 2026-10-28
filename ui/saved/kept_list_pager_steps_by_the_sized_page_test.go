package saved

import (
	"html"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/ui/page"
)

// composedPerPage is the page size one composition hands Mount through Options. The
// case keeps it away from 1 and from the number of rows an answer carries so that a
// step drawn from the rows, a step drawn from the default, and a step drawn from this
// figure are three different addresses.
const composedPerPage = 2

// TestTheKeptListPagerStepsByThePageTheCompositionSized asks the three figures the
// kept list works out for itself, with nothing above it deciding them: how far on the
// way to older copies is, and whether there is an older page at all. They come out of
// two numbers the request and the answer carry apart — the offset the page was asked
// at, and the rows that came back, which fall short of a page whenever a list ends
// mid-page — and the pager is the only way on to a second page, because no other
// control on this grid asks for an offset.
//
// Three of the pages a person walking their own copies stands on are asked of the grid
// at that page size of two: one in the middle of a filtered pair with more behind it,
// the last page of the same pair when it ends with a single copy, and a first page
// that holds everything this person kept. A step measured by the rows answered rather
// than by the page sized would walk a person back over a copy they have just read,
// and an offer kept beside a short last page leads to a grid holding nothing.
func TestTheKeptListPagerStepsByThePageTheCompositionSized(t *testing.T) {
	at := At{List: "/app/keeping/saved", Save: "/app/keeping/saved/save", Remove: "/app/keeping/saved"}
	const pair = "module=look&kind=outfit"
	for _, ask := range []struct {
		name  string
		in    filters
		rows  int
		total int64
		want  []string
	}{
		{name: "a page in the middle of a pair", in: filters{Module: "look", Kind: "outfit", Offset: 2},
			rows: 1, total: 6, want: []string{at.List + "?" + pair + "&offset=0", at.List + "?" + pair + "&offset=4"}},
		{name: "the last page of a pair, ending short", in: filters{Module: "look", Kind: "outfit", Offset: 4},
			rows: 1, total: 5, want: []string{at.List + "?" + pair + "&offset=2"}},
		{name: "a first page that holds every copy kept", in: filters{},
			rows: 2, total: 2, want: nil},
	} {
		view := grid(page.Request{}, page.Shell{BackLabel: "Workspace"}, Options{Workspace: "/app"},
			at, keptCopies(ask.rows, ask.total), ask.in, composedPerPage)
		if view.Status != http.StatusOK {
			t.Errorf("%s: the kept list answered %d, not the page of copies it draws", ask.name, view.Status)
			continue
		}
		if got := pagerHrefs(t, view.Body); !slices.Equal(got, ask.want) {
			t.Errorf("%s: the kept list offered %v, want %v", ask.name, got, ask.want)
		}
	}
}

// keptCopies is one person's page as the keeping port answers it. The cards are
// identical because this case asks where the page points, not what it draws.
func keptCopies(rows int, total int64) Page {
	out := Page{Total: total}
	for i := 0; i < rows; i++ {
		out.Rows = append(out.Rows, Row{
			ID: "5b1f1e07-9a44-4f2b-9f2a-2a2f0f0d3f4c", Module: "look", Kind: "outfit", Key: "one",
			Title: "A kept copy", Captured: time.Unix(1700000000, 0).UTC(),
			Availability: AvailabilityAvailable, SourceURL: "/app/look/outfits/one", Revision: 3,
		})
	}
	return out
}

// queryHref is one address the page offers with a query on it. Only the pager writes
// one: a card links to the record it is a copy of, its give-back posts to a form
// action, and the breadcrumb's link to the workspace carries nothing.
var queryHref = regexp.MustCompile(`href="([^"]*\?[^"]*)"`)

// pagerHrefs are the onward addresses the grid drew, in the order it drew them, with
// the escaping a browser reads undone so that a pair is compared as the page built it.
func pagerHrefs(t *testing.T, body []g.Node) []string {
	t.Helper()
	var out []string
	for _, n := range body {
		var page strings.Builder
		if err := n.Render(&page); err != nil {
			t.Fatalf("the kept list could not be rendered: %v", err)
		}
		for _, href := range queryHref.FindAllStringSubmatch(page.String(), -1) {
			out = append(out, html.UnescapeString(href[1]))
		}
	}
	return out
}
