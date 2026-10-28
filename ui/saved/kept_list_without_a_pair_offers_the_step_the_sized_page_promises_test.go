package saved

import (
	"net/http"
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/ui/page"
)

// TestTheKeptListWithoutAPairOffersTheStepTheSizedPagePromises holds the two addresses of
// the list a person lands on — every copy they kept, no module pair named — to the page the
// composition sized. What those addresses look like is held beside this case: how many a page
// of forty copies owes, and that the one filter on each is an offset this page reads. What
// nothing held is how far each of them steps, because every case naming exact addresses names
// a pair and so reads gridAt's other branch; the unfiltered branch could point both of its
// links at the first page, or measure the way back by the rows that came back rather than by
// the page sized, and every case on this grid would answer ok.
//
// Three pages a person walking their own copies stands on are asked at a page size of 24 over
// forty copies: one page in, one copy in, and a last page holding four. The offset and the row
// count are kept apart from one another and from that page size, so a step drawn from the rows,
// a step drawn from the default and a step drawn from this figure are three different addresses.
// No arm is asked at an offset below the first copy: those are the answers a clamp moved to the
// request boundary would change, and what this case holds is how far the pager steps, not where
// the raw figure is bounded.
func TestTheKeptListWithoutAPairOffersTheStepTheSizedPagePromises(t *testing.T) {
	at := At{List: "/app/keeping/saved", Save: "/app/keeping/saved/save", Remove: "/app/keeping/saved"}
	for _, ask := range []struct {
		name  string
		in    filters
		rows  int
		total int64
		want  []string
	}{
		{name: "one page in, a page back is the first copy", in: filters{Offset: 24},
			rows: 24, total: 40, want: []string{at.List + "?offset=0"}},
		{name: "one copy in, with a page behind it and one ahead", in: filters{Offset: 1},
			rows: 24, total: 40, want: []string{at.List + "?offset=0", at.List + "?offset=25"}},
		{name: "a last page of four, its way back measured by the page", in: filters{Offset: 36},
			rows: 4, total: 40, want: []string{at.List + "?offset=12"}},
	} {
		view := grid(page.Request{}, page.Shell{BackLabel: "Workspace"}, Options{Workspace: "/app"},
			at, keptCopies(ask.rows, ask.total), ask.in, 24)
		if view.Status != http.StatusOK {
			t.Errorf("%s: the kept list answered %d, not the page of copies it draws", ask.name, view.Status)
			continue
		}
		if got := pagerHrefs(t, view.Body); !slices.Equal(got, ask.want) {
			t.Errorf("%s: the kept list offered %v, want %v", ask.name, got, ask.want)
		}
	}
}
