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
	"github.com/google/uuid"

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
	// The count is this writer's only where this writer is the answer. Inside the
	// mounted API the response is held in a buffer and respond counts the status the
	// client was finally given, once — which is what keeps a 500 that replaced a held
	// 200, and the csrf refusal that never reached a router, one refusal each rather
	// than two. See countRefusal.
	if _, held := bufferFrom(r.Context()); !held {
		countRefusal(r.Context(), status)
	}
	id := requestIDFrom(r.Context())
	if a.show(w, r, id, problem.New(status, detail), false) {
		return
	}
	writeProblem(w, status, id, detail)
}

// Siteless answers a failure of the process itself to a request that names no site: the
// two probes. Same negotiation, same renderer, same single JSON encoder as fail; the one
// thing it withholds is the host lookup, which is exactly the query a probe must never
// make — kit/health's package comment gives the outage it caused, and
// TestTheProbesNeverResolveTheHost is the case that holds it. So this is not a third
// answer to what a refusal is; it is fail with the one step a probe cannot pay for taken
// out, and the page it renders has no tenant for the same reason its JSON answer never
// had one.
//
// kit/health is its only caller, through the one-method port that package declares.
// A route that has a host to resolve has a fail to call instead.
//
// It is handed the refusal rather than a status and a sentence because the two callers
// of a refusal are not owed the same words: problem.Problem's Detail is what the monitor
// diffs and the log quotes, and its Public half is what a person is shown, for the one
// verdict whose detail names an internal check nobody standing at a browser can act on.
// The document below writes Detail; the page reads Public. Nothing is invented here — a
// probe that says nothing about which check broke is the answer this port already gave to
// a person, and the reason kit/health logs it with its request id.
//
// The one thing that is not fail's here is the machine-readable answer. A page rendered
// for a probe carries the reference, minted behind the negotiation as everywhere else;
// the JSON answer withholds the instance member and the encoder's trailing newline,
// because those bytes are what a readiness stanza has compared since it existed — see
// writeProbeProblem. The person who is *shown* the failure and the monitor that parses
// it are given the same verdict by the same encoder; what a monitor is owed is that the
// bytes it reads do not move under it.
func (a *API) Siteless(w http.ResponseWriter, r *http.Request, refused *problem.Problem) {
	id := requestIDFrom(r.Context())
	if a.show(w, r, id, refused, true) {
		return
	}
	writeProbeProblem(w, refused.Status, refused.Detail)
}

// show asks the registered renderer for a page and reports whether it answered.
// It writes nothing itself, which is the whole shape of it: a renderer that declines
// leaves the refusal exactly as it was, and the caller — fail with a ResponseWriter,
// refuse with a huma context — writes the one problem document the request is owed.
// A caller that asked this and then wrote a document of its own answered the same
// refusal twice into one body, which is bytes no JSON parser accepts.
//
// This is where the refusal's tenant is resolved (withHostTenant), and it is the one
// place the question can be asked without being asked twice: it is the only branch that
// renders a page, and a page has a language. A refusal handed to a program is a code,
// the same in every language, and owes no lookup.
func (a *API) show(w http.ResponseWriter, r *http.Request, id string, refused *problem.Problem, siteless bool) bool {
	if a.opts.Fault == nil || !WantsDocument(r) {
		return false
	}
	// The mint sits behind the negotiation on purpose. A page is the only answer
	// that shows a reference, and a request that asked for a value is owed the
	// bytes it always got — no new header, no instance member. Everything except
	// the two probes already has an id by here, because the id middleware runs
	// ahead of every route; a probe is the request that reaches this line without
	// one, and the page it renders is worthless without a reference to quote.
	if id == "" {
		id = uuid.NewString()
		w.Header().Set(RequestIDHeader, id)
	}
	if !siteless {
		r = a.withHostTenant(r)
	}
	refused.Instance = "urn:request:" + id
	return a.opts.Fault(w, r, refused)
}

// refuse is fail for the guards that run inside the huma chain: the authorization
// denial, the public write limit, the host and control-plane gates, the transaction
// that could not be opened or did not commit.
//
// Those guards hold a huma.Context where the ones above hold a ResponseWriter, and one
// thing follows that the one-line version of this fix — unwrap and call fail — gets
// wrong, which is a 500 where a refusal was meant to be. The transaction middleware
// decides commit or rollback on the status this response carries, and a refusal that
// reached only the writer leaves that verdict at zero: an undecided response is rolled
// back and replaced, so the refusal the caller was shown disappears and an outage takes
// its place. declared is what carries the verdict to both places, and ctx.SetStatus is
// the same call the anonymous-caller branch of authorize makes when it answers 303.
//
// The document is written by writeProblem, the one encoder this package has, and not by
// huma's writer. That substitution is the reason this function is not just fail: huma's
// writer answers this shape its own way — a "$schema" member inside the body and a Link
// response header — so a refusal of an address that *is* served here would arrive in a
// different shape, another Content-Length and one more header, from the refusal of an
// address that is not served at all. kit/problem promises one error shape and README.md
// promises that the control plane's 404 cannot be told from a never-mounted address; both
// promises are this line, and a second encoder of the same body is what broke them. The
// answer a client that asked for a value gets from the guards is therefore now the
// shorter one, and it is the one every kernel-side refusal has always written.
func (a *API) refuse(ctx huma.Context, status int, detail string) {
	// No count here. Every refusal this writes is an answer of the mounted API, and
	// respond counts those once, from the status the client was finally given, after
	// the buffer has settled — so the guard's 403, the plan gate's 402 and the
	// transaction's 500 arrive in the number beside the answers huma writes for
	// itself, which no writer in this package sees. The status set below is what
	// makes that visible to it.
	//
	// The unwrap sits behind the same test show makes, because it is the reason to
	// reach past the huma context and it panics on a foreign one.
	r, w := humachi.Unwrap(ctx)
	id := requestIDFrom(r.Context())
	if WantsDocument(r) {
		// The verdict is stated before the page is asked for, because a page the
		// renderer wrote never passed through a writer that states one. A renderer
		// that declines has written nothing, which is what lets the document below
		// answer in the same buffer.
		ctx.SetStatus(status)
		if a.show(w, r, id, problem.New(status, detail), false) {
			return
		}
	}
	writeProblem(declared{ResponseWriter: w, ctx: ctx}, status, id, detail)
}

// declared is the writer of a refusal made inside the huma chain: the headers and the
// body are the writer huma is carrying, and the status line goes to the huma context as
// well, because that context's status — shared by every copy of it — is the verdict the
// transaction middleware reads to decide commit or rollback. Writing the status to the
// writer alone would hold a response the kernel cannot decide about; writing it to the
// context alone would write it twice into the same response.
type declared struct {
	http.ResponseWriter
	ctx huma.Context
}

func (d declared) WriteHeader(status int) { d.ctx.SetStatus(status) }

// WantsValue reports the mirror half of the same negotiation: a client that named a
// JSON media type more generously than any markup is asking to be *handed* the answer. It
// is exported for ui/page, which mounts a page as an ordinary operation and therefore has
// to decide the same question when one of its handlers refuses — but the two halves are
// not the same question, and the difference is deliberate.
//
// WantsDocument answers "did this caller ask to be shown something", and only an
// explicit text/html counts, because that is the question a *guard* asks ahead of
// routing, where the default answer for a curl or an SDK is the machine-readable one.
// WantsValue answers "did this caller refuse the page", and only an explicit JSON media
// type counts, because the caller asking it is a route that *owns* a page: a client that
// named nothing (a bare httptest request, a health-style client, anything that sent no
// Accept at all) is not refusing markup, it simply has no opinion, and the page is the
// answer this route exists to give. htmx names nothing here either: the controllers
// inside a page read the refusal's code back out of the document the kernel writes for a
// guard (ui/assets/js/htmx-config.js), and the fragments they swap are the success path.
//
// So the two predicates are one rule read from each side — a refusal is a page for a
// caller that came to look at one, and a problem document for one that named a value more
// generously than it named markup — and both readings live here rather than in the caller
// that needed them. They read the header through one function, offered, because two
// readers of the same Accept header are two answers to when each one wins, and the two
// answers drift on exactly the requests nobody thought to write a case for: the one that
// names both representations with weights.
func WantsValue(r *http.Request) bool {
	if r == nil || r.Header.Get("HX-Request") == "true" {
		return false
	}
	markup, value := offered(r)
	return value > markup
}

// offered is the one reading of a request's Accept header this kernel has: the highest
// weight the caller gave markup, and the highest weight it gave a machine-readable value,
// each negative when it named nothing of that kind. Both predicates read the header here
// so the weights are compared, not the order the caller happened to write its entries
// in: `application/json;q=0.5, text/html;q=1` and `text/html;q=1,
// application/json;q=0.5` are one preference stated twice, and a negotiation that looked
// at the first entry it liked answered them with two different documents.
//
// `q=0` is an explicit refusal rather than a low preference, so an entry weighted to zero
// never counts as offered — that is the reading text/html;q=0 has always had here, now
// read the same way on both sides. An unparsable weight falls back to 1, which is what
// RFC 9110 says a missing weight means.
//
// A wildcard is deliberately in neither set. `*/*` is what curl, monitors, SDKs and
// health checks send; counting it as markup would answer machines with a page, and
// counting it as a value would answer a browser-shaped `text/html,*/*` with a body — the
// same asymmetry WantsDocument's comment has always stated, now stated once.
func offered(r *http.Request) (markup, value float64) {
	markup, value = -1, -1
	for _, entry := range strings.Split(r.Header.Get("Accept"), ",") {
		media, params, _ := strings.Cut(strings.TrimSpace(entry), ";")
		weight := 1.0
		for _, parameter := range strings.Split(params, ";") {
			// Only `q` carries a weight; a charset or an extension says nothing about
			// how much the caller wants the media type. A bad weight is a missing one.
			name, value, named := strings.Cut(parameter, "=")
			if !named || !strings.EqualFold(strings.TrimSpace(name), "q") {
				continue
			}
			if parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
				weight = parsed
			}
		}
		if weight <= 0 {
			continue
		}
		switch {
		case strings.EqualFold(media, "text/html"), strings.EqualFold(media, "application/xhtml+xml"):
			markup = max(markup, weight)
		case strings.EqualFold(media, "application/json"),
			strings.EqualFold(media, problem.ContentType),
			strings.EqualFold(media, "application/ld+json"):
			value = max(value, weight)
		}
	}
	return markup, value
}

// WantsDocument reports whether the client asked to be *shown* the answer rather than
// handed a value.
//
// It is exported because the question is the kernel's and there is one answer to it.
// ui/page asks it of a refusal a *page handler* made (Serve): a page route answers a
// browser with a page, and a client that named application/json with the problem
// document — the same division this function decides for every guard's refusal. A
// second copy of the rule in ui would be a second answer to which client gets which
// shape, and the two would drift exactly where they matter, which is the htmx line and
// the `q=0` line below.
//
// Only an explicit text/html (or its XHTML sibling) counts. `Accept: */*` deliberately
// does not: that is what curl, health checks, SDKs and monitoring send, and answering
// them with a page would break exactly the clients that need the machine-readable body.
//
// A media type may carry parameters before its weight — `text/html;charset=utf-8;q=0.4`
// is one offer of markup at 0.4, and `;q=0` after a charset is as much a refusal of it
// as `;q=0` on its own. Reading the suffix as one string instead of as parameters is the
// bug this file exists to fix, got back by omission: the weight was only ever seen when
// it came first, so an offer weighted to zero counted at 1.
//
// A weight decides between the two sides when a caller names both: markup counts when
// it is offered at least as generously as any JSON media type is, and a caller that
// weighted `application/json` above `text/html` is asking to be handed a value. Reading
// the header as a set instead of weighing it is what made the answer depend on the order
// two entries happened to be written in — see offered, which is the one place that
// number is read, and WantsValue, which is the same reading from the other side.
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
func WantsDocument(r *http.Request) bool {
	if r == nil {
		// No request means no client asked to be shown anything — which is the answer a
		// page handler reached from a context that carried no request has always been
		// given: render the verdict, since nothing is waiting to parse a body.
		return true
	}
	if r.Header.Get("HX-Request") == "true" {
		return false
	}
	markup, value := offered(r)
	return markup >= value && markup > 0
}
