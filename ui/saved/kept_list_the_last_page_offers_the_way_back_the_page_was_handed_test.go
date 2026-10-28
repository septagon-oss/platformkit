package saved

import (
	"net/http"
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/ui/page"
)

// handedLastPage is a page size a composition hands Mount through Options.PerPage. It is
// kept apart from defaultPerPage, the page this package falls back to when none is named,
// and from the rows an answer carries: the rows are the two a kept list of thirty-five
// copies leaves over at an offset of thirty-three, which is the only way this port really
// answers short, so a way back measured by the rows is a third address again.
const handedLastPage = 5

// TestTheKeptListOnItsLastPageOffersTheWayBackOfThePageItWasHanded asks the kept list a
// person lands on — every copy they kept, no pair named — the last page of the page it was
// handed, where the way forward is owed nothing and the way back is owed the page. Every
// arm of it is a page this surface's own read answers: rows are however many copies stand
// between the offset and the last one, so the short page here is the end of the list and
// not a read that fell short of a full page, and the way back is the only address owed.
// Asked at a page of five over forty copies and one of five over ten, with the offset, the
// rows and that page kept apart from one another and from the fallback of 24, so a way back
// drawn from the rows, from the fallback and from this figure are three different answers.
func TestTheKeptListOnItsLastPageOffersTheWayBackOfThePageItWasHanded(t *testing.T) {
	at := At{List: "/app/keeping/saved", Save: "/app/keeping/saved/save", Remove: "/app/keeping/saved"}
	for _, ask := range []struct {
		name  string
		in    filters
		rows  int
		total int64
		want  []string
	}{
		{name: "the second page of ten, ending at the last copy", in: filters{Offset: 5},
			rows: 5, total: 10, want: []string{at.List + "?offset=0"}},
		{name: "thirty-three copies in, two of thirty-five left to show", in: filters{Offset: 33},
			rows: 2, total: 35, want: []string{at.List + "?offset=28"}},
	} {
		view := grid(page.Request{}, page.Shell{BackLabel: "Workspace"}, Options{Workspace: "/app"},
			at, keptCopies(ask.rows, ask.total), ask.in, handedLastPage)
		if view.Status != http.StatusOK {
			t.Errorf("%s: the kept list answered %d, not the last page of its copies", ask.name, view.Status)
			continue
		}
		if got := pagerHrefs(t, view.Body); !slices.Equal(got, ask.want) {
			t.Errorf("%s: the kept list offered %v, want the one way back %v", ask.name, got, ask.want)
		}
	}
}
