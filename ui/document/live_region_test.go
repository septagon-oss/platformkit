package document_test

import (
	"strings"
	"testing"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/septagon-oss/platformkit/ui"

	"github.com/septagon-oss/platformkit/ui/document"
)

// The two elements a live page is drawn as, and what each attribute is for: the
// owner holds the connection and nothing swaps it; the region is what a refresh
// replaces, and it survives its own refresh because the swap is into it rather
// than of it. The values are the browser's contract with this kernel — which
// extension, which address, which frame names — so they are asserted as the bytes
// a browser reads rather than as a structure that could hold them any which way.
func TestALiveRegionIsAnOwnerAndTheRegionItFeeds(t *testing.T) {
	t.Parallel()
	region := document.LiveRegion("/api/v1/app/stream?topics=notification.created",
		"/app/notification/notifications", []string{"notification.created"},
		[]g.Node{h.H2(g.Text("Notifications"))})
	out := render(t, region)
	for _, want := range []string{
		`<div hx-ext="sse" sse-connect="/api/v1/app/stream?topics=notification.created">`,
		`<div hx-get="/app/notification/notifications" hx-swap="innerHTML" hx-trigger="refresh"`,
		`sse-swap="notification.created,resync" data-controller="live">`,
		`<h2>Notifications</h2></div></div>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("a live region lacks %s\n%s", want, out)
		}
	}
	// The region is inside the owner: a swap that reached the owner would close
	// the connection that caused it.
	if strings.Index(out, `data-controller="live"`) < strings.Index(out, `hx-ext="sse"`) {
		t.Errorf("the region element precedes the element holding its connection:\n%s", out)
	}
}

// resync is not a topic a module declares — it belongs to the connection — and
// every region watches it, because it is the frame that says this connection is
// not current and reading again is the whole of the remedy.
func TestEveryLiveRegionWatchesTheResyncFrame(t *testing.T) {
	t.Parallel()
	out := render(t, document.LiveRegion("/api/v1/app/stream?topics=task.closed", "/app/task/tasks",
		[]string{"task.closed"}, nil))
	if !strings.Contains(out, `sse-swap="task.closed,resync"`) {
		t.Errorf("a region watches only what it declared, and not the frame that says it is stale:\n%s", out)
	}
	if strings.Contains(out, `sse-connect="/api/v1/app/stream?topics=task.closed,resync"`) {
		t.Errorf("the connection was told to watch resync, which no module declares:\n%s", out)
	}
}

// A page that names the events its body is a region for loads the pinned extension;
// every other page loads nothing, which is the half of the promise that says a live
// region costs a page that has none nothing at all.
func TestTheExtensionIsLoadedByThePageThatNamesATopic(t *testing.T) {
	t.Parallel()
	live := render(t, document.Document(chrome(), document.Request{},
		document.View{Title: "Notifications", Live: []string{"notification.created"}}, h.Main()))
	if !strings.Contains(live, `src="/admin/assets/js/`+ui.SSEExtension+`" type="module">`) {
		t.Errorf("a live page does not load the pinned extension:\n%s", live)
	}
	// A module script and a deferred classic script share one in-order list that
	// runs before DOMContentLoaded, which is when htmx reads the document: the
	// extension is defined before the page's hx-ext="sse" is ever looked up.
	if strings.Index(live, "htmx.min.js") > strings.Index(live, "htmx-ext-sse") {
		t.Errorf("the extension is fetched ahead of htmx itself, which is the one order that cannot work:\n%s", live)
	}

	quiet := render(t, document.Document(chrome(), document.Request{},
		document.View{Title: "Ordinary"}, h.Main()))
	for _, unwanted := range []string{"htmx-ext-sse", "hx-ext", "sse-connect", "sse-swap", `data-controller="live"`} {
		if strings.Contains(quiet, unwanted) {
			t.Errorf("a page with no live region carries %s:\n%s", unwanted, quiet)
		}
	}
}
