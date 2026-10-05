package httpx

// respond.go is the outermost middleware: it turns a panic into a problem
// document and refuses to let a public route's cookie reach the client.

import (
	"context"
	"encoding/json"
	"net/http"
	"runtime/debug"
	"strings"

	"github.com/septagon-oss/platformkit/kit/problem"
)

// respond decides what the client finally sees.
//
// It holds the response in a buffer until the handler and its transaction are
// both finished, and it turns a panic into a 500 and a log line with the
// request id, so one bad handler costs one request instead of the process.
//
// The two belong together: a recovery is only able to answer at all because
// nothing has been sent yet. It sits outside the transaction, which is the
// other half of the point — a recovery any deeper would return normally, and a
// transaction that returns normally commits the half-finished work that caused
// the panic. Here the panic unwinds past the transaction middleware, which
// rolls back on its way out, and arrives with an empty buffer.
// rolls back on its way out, and arrives with an empty buffer.

// renegotiate replaces a held refusal that huma wrote with the answer this package
// would have written for the same verdict. The guard is the shape, not the status: a
// body this package's encoder wrote is already the one answer (fault.go's fail, refuse
// and Siteless all end at writeProblem), so re-writing it would be two writers again.
// The status the line carries and the status the body names have to agree, because the
// verdict is the one thing a replacement may not move.
//
// The body is discarded and the encoder's two headers with it; every other header
// stays, because those are the guard's answer and not the encoder's. Retry-After is the
// one that matters: a refusal that says "come back in 8 seconds" in its body and drops
// the number from its headers tells a client to stop and not when — which is what
// buffer's reset would have done, reset being the tool for a held 200 whose transaction
// failed, where the headers beside it are the thing that must not survive.
func (a *API) renegotiate(b *buffer, r *http.Request) {
	if a.opts.Fault == nil || b.direct || b.status < http.StatusBadRequest ||
		!strings.HasPrefix(b.Header().Get("Content-Type"), problem.ContentType) ||
		!WantsDocument(r) {
		return
	}
	var refused problem.Problem
	if err := json.Unmarshal(b.body.Bytes(), &refused); err != nil || refused.Status != b.status {
		return
	}
	b.body.Reset()
	b.Header().Del("Content-Type")
	b.Header().Del("Link") // huma points its own $schema at a document this body does not carry
	a.fail(b, r, refused.Status, refused.Detail)
}

func (a *API) respond(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := &buffer{ResponseWriter: w, header: w.Header().Clone(), public: SurfaceOf(r.Context()) == SurfacePublic}
		r = r.WithContext(context.WithValue(r.Context(), bufferKey{}, b))
		// The tenant this request resolves goes down with the response, for the
		// count below: see answerNote.
		note := &answerNote{}
		r = r.WithContext(context.WithValue(r.Context(), answerNoteKey{}, note))
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v) // net/http's own signal for "drop this connection quietly"
				}
				a.rlog(r.Context()).ErrorContext(r.Context(), "httpx: handler panicked",
					"method", r.Method, "path", r.URL.Path, "panic", v, "stack", string(debug.Stack()))
				if b.reset() {
					a.fail(b, r, http.StatusInternalServerError, "")
				}
			}
			b.send()
			// The count is here rather than at the writer that answered, because this
			// is the one place that knows what the client was finally given: the buffer
			// may have replaced what any of the writers below it chose, and a refusal
			// counted for an answer nobody received is a number an operator would be
			// wrong to read. It is after send rather than before, because an answer that
			// could not be written is not an answer either.
			if b.status >= http.StatusBadRequest {
				countRefusal(note.context(r.Context()), b.status)
			}
		}()
		next.ServeHTTP(b, r)

		// The public surface sets no cookie. It is a promise the surface makes to
		// a visitor — nothing about this request is remembered, which is what lets
		// it be cached and indexed — and a handler that minted one broke it on a
		// page a crawler will keep. The writer is where the promise is kept (see
		// withholdCookies): the cookie never reaches the visitor, whatever size the
		// body turned out to be. What is left here is the honest *answer*: where
		// the response is still held, it becomes a 500 naming the fault with the
		// body discarded, because a route that broke the surface's own promise has
		// not produced a response worth caching. Where the bytes already went, the
		// status is one the visitor has seen and the line names the route for
		// whoever owns it.
		// The last chance to answer a refusal in the shape the caller asked for. huma
		// writes the refusals it makes itself — a body over the operation's bound, a body
		// that will not decode, a handler's 5xx — with its own encoder, past the reach of
		// fault.go, because the negotiation lives where the request is and huma's writer is
		// reached from inside generated code. This is the one place above all of them that
		// holds the finished answer, so a problem document that arrived here from huma is
		// read back and answered through the kernel's one writer, and the client that came
		// to look at a page is not sent a body to parse. A caller that named a value is
		// untouched, and so is any response that has begun reaching the wire.
		a.renegotiate(b, r)
		if minted := b.withheld(); len(minted) > 0 {
			a.rlog(r.Context()).ErrorContext(r.Context(), "httpx: a public route set a cookie",
				"code", CodePublicSetsACookie, "method", r.Method, "path", r.URL.Path, "cookies", len(minted))
			if b.reset() {
				a.fail(b, r, http.StatusInternalServerError,
					CodePublicSetsACookie+": the public surface promises nobody is remembered, and this route broke the promise")
			}
		}
	})
}

// writeProblem answers with the one error shape. It is the only encoder of it: the
// recovery runs outside any huma context, a panic during routing has none at all, and a
// refusal inside the chain reaches it through declared, because huma's writer stamps a
// schema link into the body and answers a header this encoder does not, which would make
// two shapes of the one problem. See fault.go.
//
// The bytes are the marshalled document and one newline, which is what an encoder
// writing into the writer produced, kept as two writes rather than one Encode so that
// writeProbeProblem below can answer with the same encoder and the same field order.
// Splitting it this way is not a second shape: it is the one document, and the only
// question the two callers answer differently is whether they end it with a newline.
func writeProblem(w http.ResponseWriter, status int, id, detail string) {
	writeProblemBody(w, status, true, id, detail)
}

// writeProbeProblem answers the two probes. It is the same document from the same
// encoder with one thing withheld: the instance URN. Not because a probe has no request
// to name — it has one, and X-Request-ID carries it as it always did — but because this
// is the body a monitor has diffed since the probe existed, and a member added to a
// document something parses and compares is a change of contract in the one place the
// contract is not a human reading a page. A person who is *shown* the failure gets the
// reference, in the page the negotiated renderer writes.
//
// It withholds the trailing newline for the same reason: an orchestrator's readiness
// stanza, a kubelet exec probe grepping stdout, a shell piping curl into jq -e — every
// one of those reads the same bytes whether or not the line ends, and every one of them
// was written against the answer this probe gave before it had a page to hand to a
// browser. Nothing here is worth breaking to make a document 503-shaped.
func writeProbeProblem(w http.ResponseWriter, status int, detail string) {
	writeProblemBody(w, status, false, "", detail)
}

func writeProblemBody(w http.ResponseWriter, status int, newline bool, id, detail string) {
	p := problem.New(status, detail)
	if id != "" {
		p.Instance = "urn:request:" + id
	}
	body, err := json.Marshal(p)
	if err != nil {
		// problem.New builds a struct of strings and one int; it does not fail. If it
		// ever did, the status line is still the verdict, and a body that is not there
		// is honest in the way a half-written one is not.
		w.Header().Set("Content-Type", problem.ContentType)
		w.WriteHeader(status)
		return
	}
	if newline {
		body = append(body, '\n')
	}
	w.Header().Set("Content-Type", problem.ContentType)
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
