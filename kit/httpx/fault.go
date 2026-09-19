package httpx

// This file is about one question the kernel kept answering twice by accident: when a
// request fails, what does the client get?
//
// Before it, the kernel wrote an RFC 9457 JSON body at every refusal it made itself —
// the cross-site guard, the panic recoverer — while a page handler that refused got a
// real document from ui/page. Same verdict, two shapes, and the one a person saw
// depended on which line noticed them. A browser navigation that a guard refused showed
// the person a JSON blob in a window: technically correct, unusable, and with a request
// id nobody could copy.
//
// The rule now: the verdict is always a *problem.Problem; the *shape* is decided by the
// client that asked. A request that came to be looked at gets whatever document the
// presentation layer registered; everything else keeps the JSON, byte for byte as
// before, because API clients and every existing test read it.
//
// The kernel may not build that document itself. kit/httpx knows nothing about styles,
// chrome or the shell, and ui/* must not be imported from here — so the document arrives
// as a function the application registers at composition, exactly like Tenants,
// Authorize and Entitle. An application that registers nothing behaves precisely as it
// did before this file existed.

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/septagon-oss/platformkit/kit/problem"
)

// Fault renders a refusal as a document, for a request that came from something that
// will show it to a person. It reports whether it answered: returning false falls back
// to the Problem JSON, which is how a renderer opts out for a request it has no chrome
// for rather than inventing one.
//
// It receives the same *problem.Problem the JSON body would have carried, including the
// instance URN with the request id. The status is the verdict's, never the renderer's
// choice: a page that says "Forbidden" while answering 200 teaches a monitoring system
// to look away.
type Fault func(w http.ResponseWriter, r *http.Request, p *problem.Problem) bool

// fail answers a refusal the kernel made for itself, in the shape the requester asked
// for. Every kernel-side refusal goes through here; a second writer of problem bodies
// in this package is a second answer to the question this file exists to ask once.
func (a *API) fail(w http.ResponseWriter, r *http.Request, status int, detail string) {
	id := requestIDFrom(r.Context())
	p := problem.New(status, detail)
	if id != "" {
		p.Instance = "urn:request:" + id
	}
	if a.opts.Fault != nil && wantsDocument(r) && a.opts.Fault(w, r, p) {
		return
	}
	writeProblem(w, status, id, detail)
}

// wantsDocument reports whether the client asked to be *shown* the answer rather than
// handed a value.
//
// Only an explicit text/html (or its XHTML sibling) counts. `Accept: */*` deliberately
// does not: that is what curl, health checks, SDKs and monitoring send, and answering
// them with a page would break exactly the clients that need the machine-readable body.
// An htmx request counts, because htmx swaps whatever it is given into the region it
// came from — which is the point of the request being a document in the first place.
func wantsDocument(r *http.Request) bool {
	if r.Header.Get("HX-Request") == "true" {
		return true
	}
	for _, offered := range strings.Split(r.Header.Get("Accept"), ",") {
		media, params, _ := strings.Cut(strings.TrimSpace(offered), ";")
		if !strings.EqualFold(media, "text/html") && !strings.EqualFold(media, "application/xhtml+xml") {
			continue
		}
		// `text/html;q=0` is an explicit refusal, not a preference.
		if q, ok := strings.CutPrefix(strings.TrimSpace(params), "q="); ok {
			if weight, err := strconv.ParseFloat(q, 64); err == nil && weight <= 0 {
				continue
			}
		}
		return true
	}
	return false
}
