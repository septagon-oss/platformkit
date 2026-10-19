package document

import (
	"slices"
	"strings"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"
)

// resyncEvent is the frame every live connection is sent when it is not current:
// once when it opens, and once when the server had to drop a notice it could not
// keep up with. It belongs to the connection rather than to any one page, so
// every region listens for it whatever else it watches, and reading the page
// again is the whole of what a person who missed something gets.
const resyncEvent = "resync"

// LiveRegion wraps a live page's body in the two elements that make it live: an
// outer owner that holds the connection, and the region that is replaced.
//
// connect is the address of the connection to open, with its topics already in
// its query; address is the region's own address, which is the one a refresh
// re-reads; topics are the event names this region is a region for — the same
// names the frames are sent under. body is the region's content, and it is the
// only part of the page a swap touches.
//
// Two elements rather than one is the whole of the contract: a swap that
// replaced the element carrying the connection would close the connection that
// caused it, so the connection sits on an element nothing swaps and the content
// sits inside it. htmx's default swap in this application is outerHTML, which is
// why innerHTML is stated explicitly — the element the listeners are on must
// survive its own refresh.
//
// The attributes are behaviour and not presentation, so neither element carries
// a class: there is no rule to write for a box that only holds other boxes, and
// the sheet gate refuses a class nothing styles.
func LiveRegion(connect, address string, topics []string, body []g.Node) g.Node {
	watched := strings.Join(append(slices.Clone(topics), resyncEvent), ",")
	return h.Div(
		g.Attr("hx-ext", "sse"),
		g.Attr("sse-connect", connect),
		h.Div(append([]g.Node{
			g.Attr("hx-get", address),
			g.Attr("hx-swap", "innerHTML"),
			g.Attr("hx-trigger", "refresh"),
			g.Attr("sse-swap", watched),
			g.Attr("data-controller", "live"),
		}, body...)...),
	)
}
