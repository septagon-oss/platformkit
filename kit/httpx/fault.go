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

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"

	"github.com/septagon-oss/platformkit/kit/problem"
)

// Fault renders a refusal as a document, for a request that came from something that
// will show it to a person. It reports whether it answered: returning false falls back
// to the Problem JSON, which is how a renderer opts out for a request it has no chrome
// for rather than inventing one. The fallback is the one document a refusal always
// gets — the same body, and the same single writer, as an application that registers no
// renderer at all. Opting out is a refusal to write, not a licence for two writers.
//
// It receives the same *problem.Problem the JSON body would have carried, including the
// instance URN with the request id. The status is the verdict's, never the renderer's
// choice: a page that says "Forbidden" while answering 200 teaches a monitoring system
// to look away.
type Fault func(w http.ResponseWriter, r *http.Request, p *problem.Problem) bool

// fail answers a refusal the kernel made for itself, in the shape the requester asked
// for. Every kernel-side refusal goes through here, and every refusal a guard inside
// the huma chain makes goes through refuse, which is the same rule holding the chain's
// writer instead of this one; a second writer of problem bodies in this package is a
// second answer to the question this file exists to ask once.
func (a *API) fail(w http.ResponseWriter, r *http.Request, status int, detail string) {
	id := requestIDFrom(r.Context())
	if a.show(w, r, id, status, detail) {
		return
	}
	writeProblem(w, status, id, detail)
}

// show asks the registered renderer for a page and reports whether it answered.
// It writes nothing itself, which is the whole shape of it: a renderer that declines
// leaves the refusal exactly as it was, and the caller — fail with a ResponseWriter,
// refuse with a huma context — writes the one problem document the request is owed.
// A caller that asked this and then wrote a document of its own answered the same
// refusal twice into one body, which is bytes no JSON parser accepts.
func (a *API) show(w http.ResponseWriter, r *http.Request, id string, status int, detail string) bool {
	if a.opts.Fault == nil || !wantsDocument(r) {
		return false
	}
	p := problem.New(status, detail)
	if id != "" {
		p.Instance = "urn:request:" + id
	}
	return a.opts.Fault(w, r, p)
}

// refuse is fail for the guards that run inside the huma chain: the authorization
// denial, the public write limit, the host and control-plane gates, the transaction
// that could not be opened or did not commit.
//
// Those guards hold a huma.Context where the ones above hold a ResponseWriter, and two
// things follow that the one-line version of this fix — unwrap and call fail — gets
// wrong, which is a 500 where a refusal was meant to be.
//
//   - The response is the one respond is holding, so the answer goes to the writer huma
//     is carrying (humachi.Unwrap) and not to a new one. A body written past the buffer
//     leaves the kernel with no status to commit against — the reason notHere has always
//     answered through huma rather than onto the writer.
//   - The transaction middleware decides commit or rollback on the status of this
//     response, and a page the renderer wrote never passed through huma's writer, so it
//     would leave the verdict at zero. An undecided response is rolled back and replaced
//     with a 500: the refusal the person was shown disappears and an outage takes its
//     place. ctx.SetStatus is what says the verdict is decided, and it is the same call
//     the anonymous-caller branch of authorize makes when it answers 303 instead.
//
// The document a client that asked for a value gets is unchanged, byte for byte, and so
// is the one an application that registers no renderer has always written. A renderer
// that was asked and declined is owed that same one document: hence ask the renderer
// here (show) rather than fail, which answers the fallback itself because a kernel-side
// caller has no huma writer to fall back on, and then this line would be the second
// writer of the same verdict.
func (a *API) refuse(ctx huma.Context, status int, detail string) {
	if a.opts.Fault != nil {
		// The unwrap sits behind the same test show makes, because it is the only
		// reason to reach past the huma context and it panics on a foreign one.
		r, w := humachi.Unwrap(ctx)
		if wantsDocument(r) {
			ctx.SetStatus(status)
			if a.show(w, r, requestIDFrom(r.Context()), status, detail) {
				return
			}
		}
	}
	_ = huma.WriteErr(a.api, ctx, status, detail)
}

// wantsDocument reports whether the client asked to be *shown* the answer rather than
// handed a value.
//
// Only an explicit text/html (or its XHTML sibling) counts. `Accept: */*` deliberately
// does not: that is what curl, health checks, SDKs and monitoring send, and answering
// them with a page would break exactly the clients that need the machine-readable body.
//
// An htmx request does not count either, which is the half of this rule that reads
// backwards — htmx asks with `Accept: text/html,*/*`. It does not ask to be *shown*
// anything: it is a controller in a page, and this composition's controller
// (ui/assets/js/htmx-config.js) configures htmx to swap nothing for a 4xx
// (`{code: "[45]..", swap: false, error: true}`) and then reads the refusal's code out
// of the problem document to choose the recovery notice and to keep the form's unsaved
// input. Answering it with a page throws the body away in the library and leaves the
// person with a form that silently did nothing; the journeys in
// e2e/session-recovery.spec.ts are what that was built for. A swapped fragment is still
// what htmx gets on a success (see Redirect and Page), so this says only that a
// refusal goes to the caller that parses it.
func wantsDocument(r *http.Request) bool {
	if r.Header.Get("HX-Request") == "true" {
		return false
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
