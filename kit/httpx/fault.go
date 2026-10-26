package httpx

// This file answers one question the kernel kept answering twice by accident: when a
// request fails, what does the client get?
//
// The rule: the verdict is always a *problem.Problem; the *shape* is decided by the client
// that asked. A request that came to be looked at gets whatever document the presentation
// layer registered, and everything else keeps the RFC 9457 JSON body byte for byte,
// because API clients and every existing test read it.
//
// The kernel may not build that document itself. kit/httpx knows nothing about styles,
// chrome or the shell, and ui/* must not be imported from here — so the document arrives as
// a function the application registers at composition, exactly like Tenants, Authorize and
// Entitle. An application that registers nothing answers as it did before this file.

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
// two probes, which kit/health reaches through the one-method port it declares. It is
// fail minus the host lookup — the one query a probe must never make, whose outage
// kit/health's package comment tells — and so the page it renders has no tenant for the
// same reason its JSON answer never had one. A route with a host to resolve calls fail.
//
// The refusal arrives as a *problem.Problem, not a status and a sentence, because its
// two readers are owed different words: Detail, which names an internal check, is what
// a monitor diffs and the log quotes; Public is what the person is shown instead.
//
// Withholding the instance member and the encoder's trailing newline is the one place
// the machine-readable answer is not fail's: those bytes are what a readiness stanza has
// compared since it existed — see writeProbeProblem.
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

// refuse is fail for the guards that run inside the huma chain: the authorization denial,
// the public write limit, the host and control-plane gates, the transaction that could not
// be opened or did not commit.
//
// Those guards hold a huma.Context where the ones above hold a ResponseWriter, and one
// thing follows that "unwrap and call fail" gets wrong, which is a 500 where a refusal was
// meant to be: the transaction middleware decides commit or rollback on the status this
// response carries, and a refusal that reached only the writer leaves that verdict at zero,
// so the refusal the caller was shown disappears under a rolled-back replacement — declared
// carries the verdict to both places.
//
// writeProblem writes the document, not huma's writer, which adds a "$schema" member and a
// Link header: kit/problem promises one error shape either way.
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
// JSON media type more generously than any markup is asking to be *handed* the answer.
// It is exported for ui/page, which mounts a page as an ordinary operation and so must
// decide the same question when one of its own handlers refuses.
//
// The two halves are not one question, deliberately. WantsDocument asks "did this
// caller ask to be shown something", where only an explicit text/html counts, because
// a guard asks it ahead of routing and the answer a curl or an SDK expects is the
// machine-readable one. WantsValue asks "did this caller refuse the page", where only
// an explicit JSON media type counts, because the caller asking it owns a page: one
// that named nothing has no opinion to honour, and the page is the answer this route
// exists to give.
//
// Both read the header through offered, so one weighing decides when each side wins.
func WantsValue(r *http.Request) bool {
	if r == nil || r.Header.Get("HX-Request") == "true" {
		return false
	}
	markup, value := offered(r)
	return value > markup
}

// offered is the one reading of a request's Accept header this kernel has: the highest
// weight the caller gave markup, and the highest weight it gave a machine-readable
// value, each negative when it named nothing of that kind. Both predicates read the
// header here so the weights are compared, not the order the caller wrote its entries
// in: `application/json;q=0.5, text/html;q=1` and the same list reversed are one
// preference stated twice, and a negotiation that took the first entry it liked
// answered them with two different documents.
//
// `q=0` is an explicit refusal rather than a low preference, so an entry weighted to
// zero never counts as offered; an unparsable weight falls back to 1, which is what
// RFC 9110 says a missing weight means.
//
// A wildcard sits in neither set on purpose — see WantsDocument and HX-Request below.
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
// handed a value. It is exported because the question is the kernel's and there is one
// answer to it; ui/page asks the mirror, WantsValue, of a refusal its own handler made.
//
// Only an explicit text/html (or its XHTML sibling) counts: `Accept: */*` is what curl,
// health checks, SDKs and monitoring send, and answering them with a page breaks the
// clients that need the machine-readable body. offered weighs both sides when a caller
// names both, and WantsValue is the same weighing read from the other side.
//
// A media type carries parameters before its weight, so `text/html;charset=utf-8;q=0.4`
// offers markup at 0.4 and `;q=0` after a charset refuses it as plainly as on its own.
// The weight was only ever seen when it came first, and reading the suffix as one string
// instead of as parameters is the bug this file exists to fix.
func WantsDocument(r *http.Request) bool {
	if r == nil {
		// No request means no client asked to be shown anything — which is the answer a
		// page handler reached from a context that carried no request has always been
		// given: render the verdict, since nothing is waiting to parse a body.
		return true
	}
	if r.Header.Get("HX-Request") == "true" {
		// Reads backwards: htmx asks with `Accept: text/html,*/*` but is only a controller in
		// a page, and ui/assets/js/htmx-config.js swaps nothing for a 4xx and reads the code
		// out of the problem body to choose the recovery — a page here loses that body.
		return false
	}
	markup, value := offered(r)
	return markup >= value && markup > 0
}
