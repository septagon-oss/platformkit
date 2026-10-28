package saved

import (
	"net/http"
	"net/url"
	"strconv"
	"testing"

	"github.com/septagon-oss/platformkit/ui/page"
)

// TestTheKeptListNeverOffersAnAddressItCannotAnswer asks the kept list for pages a
// person may stand on — one an offset they typed themselves, the only figure on this
// grid that arrives as a number nobody clicked — and holds that every address it draws
// is one the kept list reads. An offset below the first copy is no page of copies: the
// service's own bounds refuse to read one and this surface answers such a request as the
// first page, so a step worked out off the raw request rather than off the page shown
// ends at an address the page would not read itself. Asked at a page size of 24 one page
// in, one copy in, and thirty copies before the first one; the pair has to still be
// named, the offset a number a reading takes, and the count of addresses the one a page
// of forty copies owes, so an answer that drew none cannot pass by offering nothing.
func TestTheKeptListNeverOffersAnAddressItCannotAnswer(t *testing.T) {
	at := At{List: "/app/keeping/saved", Save: "/app/keeping/saved/save", Remove: "/app/keeping/saved"}
	for _, ask := range []struct {
		name   string
		offset int
		want   int
	}{
		{name: "one page in, a page back is the first copy", offset: 24, want: 1},
		{name: "one copy in, a number nobody clicked", offset: 1, want: 2},
		{name: "thirty copies before the first one", offset: -30, want: 1},
	} {
		view := grid(page.Request{}, page.Shell{BackLabel: "Workspace"}, Options{Workspace: "/app"},
			at, keptCopies(24, 40), filters{Module: "look", Kind: "outfit", Offset: ask.offset}, 24)
		if view.Status != http.StatusOK {
			t.Errorf("%s: the kept list answered %d, not the page of copies it draws", ask.name, view.Status)
			continue
		}
		got := pagerHrefs(t, view.Body)
		if len(got) != ask.want {
			t.Errorf("%s: the kept list offered %d addresses %v, want %d", ask.name, len(got), got, ask.want)
			continue
		}
		for _, href := range got {
			q, err := url.Parse(href)
			if err != nil {
				t.Errorf("%s: %s is no address at all: %v", ask.name, href, err)
				continue
			}
			if q.Query().Get("module") != "look" || q.Query().Get("kind") != "outfit" {
				t.Errorf("%s: %s no longer names the pair the list was asked for", ask.name, href)
			}
			if off, err := strconv.Atoi(q.Query().Get("offset")); err != nil || off < 0 {
				t.Errorf("%s: %s carries an offset this page would have to clamp before reading it: %v",
					ask.name, href, err)
			}
		}
	}
}
