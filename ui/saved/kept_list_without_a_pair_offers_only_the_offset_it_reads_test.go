package saved

import (
	"net/http"
	"net/url"
	"strconv"
	"testing"

	"github.com/septagon-oss/platformkit/ui/page"
)

// TestTheKeptListWithoutAPairOffersOnlyTheOffsetItReads asks the list a person lands on:
// every copy they kept, no pair named. gridAt builds that address on a branch of its own,
// and it is the branch no other case on this grid reaches, so both of its promises are
// unreached without this file: that the address carries the offset and nothing else — a
// link carrying anything else promises a filter this page does not read — and that the
// offset on it is one this page reads, which an offset typed below the first copy would
// not be. Asked one page in, one copy in, and thirty copies before the first one, at a
// page size of 24 over forty copies, and the count is held before the addresses are read,
// so an answer that drew none cannot pass by offering nothing.
func TestTheKeptListWithoutAPairOffersOnlyTheOffsetItReads(t *testing.T) {
	at := At{List: "/app/keeping/saved", Save: "/app/keeping/saved/save", Remove: "/app/keeping/saved"}
	for _, ask := range []struct {
		name   string
		offset int
		want   int
	}{
		{name: "one page in", offset: 24, want: 1},
		{name: "one copy in, a number nobody clicked", offset: 1, want: 2},
		{name: "thirty copies before the first one", offset: -30, want: 1},
	} {
		view := grid(page.Request{}, page.Shell{BackLabel: "Workspace"}, Options{Workspace: "/app"},
			at, keptCopies(24, 40), filters{Offset: ask.offset}, 24)
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
			if len(q.Query()) != 1 {
				t.Errorf("%s: %s carries %v, want the one filter this page reads", ask.name, href, q.Query())
			}
			if off, err := strconv.Atoi(q.Query().Get("offset")); err != nil || off < 0 {
				t.Errorf("%s: %s carries an offset this page would have to clamp before reading it: %v",
					ask.name, href, err)
			}
		}
	}
}
