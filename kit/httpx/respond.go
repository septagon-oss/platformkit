package httpx

// respond.go is the outermost middleware: it turns a panic into a problem
// document and refuses to let a public route's cookie reach the client.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"runtime/debug"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/septagon-oss/platformkit/kit/problem"
)

// respond buffers the response until the handler and transaction finish. It
// recovers panics outside the transaction middleware so the transaction rolls
// back before the buffered response is replaced with a refusal.

// renegotiate replaces a problem document with the configured page response when
// the request accepts a document. It retains response headers from the guard and
// uses the carried refusal when available so the page can include its detail.
func (a *API) renegotiate(b *buffer, r *http.Request) {
	if a.opts.Fault == nil || b.direct || b.status < http.StatusBadRequest ||
		!strings.HasPrefix(b.Header().Get("Content-Type"), problem.ContentType) ||
		!WantsDocument(r) {
		return
	}
	refused := b.refusal()
	if refused == nil || refused.Status != b.status {
		var written problem.Problem
		if err := json.Unmarshal(b.body.Bytes(), &written); err != nil || written.Status != b.status {
			return
		}
		refused = &written
	}
	b.body.Reset()
	b.Header().Del("Content-Type")
	b.Header().Del("Link") // huma points its own $schema at a document this body does not carry
	id := requestIDFrom(r.Context())
	if a.show(b, r, id, refused, false) {
		return
	}
	// The renderer declined: it has no chrome for this request, and it wrote nothing. The
	// held response is empty now, so the refusal goes back through the kernel's one
	// encoder, which is the answer fail ends in and the shape every refusal of this
	// address has always had — no $schema member, no Link, no second writer.
	writeProblem(b, refused.Status, id, refused.Detail)
}

// noteCarried tells the held response which refusal its handler answered with, which is
// the only way the page this response may become learns what the problem document it is
// built from does not say. See buffer.carried.
//
// The test below is huma's own: the framework writes the StatusError errors.As finds and
// turns anything else into a 500 about its cause, so this records the verdict only where
// the framework is about to answer with this very object. A problem reachable only through
// %w is a cause, and the response's verdict is the framework's, not the buried one.
func noteCarried(ctx context.Context, err error) {
	if err == nil {
		return
	}
	var refused *problem.Problem
	if !errors.As(err, &refused) {
		return
	}
	var stated huma.StatusError
	if !errors.As(err, &stated) || stated != huma.StatusError(refused) {
		return
	}
	if b, ok := bufferFrom(ctx); ok {
		b.carry(refused)
	}
}

func (a *API) respond(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := &buffer{ResponseWriter: w, header: w.Header().Clone(), public: SurfaceOf(r.Context()) == SurfacePublic}
		r = r.WithContext(context.WithValue(r.Context(), bufferKey{}, b))
		// The tenant this request resolves goes down with the response, for the
		// count below: see answerNote.
		note := &answerNote{}
		r = r.WithContext(context.WithValue(r.Context(), answerNoteKey{}, note))
		// The keyed write's own slot, held for the same reason and beside the same
		// buffer: the guard that takes the claim runs below here and holds a
		// huma.Context, so the claim it took — and with it the context the store read
		// its connection from — travels outward to the one defer that knows what the
		// client was finally given. Exactly one slot per request, because the guard
		// fills the value this function's defer reads: a second install would leave one
		// slot filled and another, read, empty.
		r = r.WithContext(withClaim(r.Context()))
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
			if n, ok := operationNote(r.Context()); ok && n.failureErr != nil && b.reset() {
				a.fail(b, r, http.StatusServiceUnavailable, "")
			}
			// A 429 that named no wait is a refusal that tells a client nothing it can
			// act on, and this is the last moment its headers are still ours. The guard
			// that decided a limit already named the window; a handler that counted
			// somewhere this package cannot see gets the floor and a line naming it.
			if b.status == http.StatusTooManyRequests && b.Header().Get(RetryAfterHeader) == "" {
				b.Header().Set(RetryAfterHeader, secondsUp(defaultRetryAfter, 1))
				a.log.WarnContext(r.Context(), "httpx: a 429 answered with no Retry-After, so the kernel said its own",
					"method", r.Method, "path", r.URL.Path, "seconds", int64(defaultRetryAfter.Seconds()),
					"why", "the route counted its own limit and did not state the wait")
			}
			// Whether the answer is still held decides what can be remembered of it:
			// past the first byte there is nothing here left to hold back, only the
			// status a caller has already seen.
			held := !b.begun()
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
			// And the answer is remembered, after the client was given it and not
			// before. This is the ordering that keeps a recorded 200 from being a
			// committed 500: everything above this line may still replace the response,
			// and what a keyed write promises is the answer the caller actually got.
			if n, ok := r.Context().Value(claimKey{}).(*claimNote); ok {
				// Read after send, because send is where a replaced answer has finished
				// being replaced: the headers and the bytes here are the ones the caller
				// was given, which is the only answer worth remembering.
				a.complete(r.Context(), n, b.status, b.Header(), b.body.Bytes(), held, b.counted)
			}
		}()
		next.ServeHTTP(b, r)

		// The public surface sets no cookie. It is a promise the surface makes to a
		// visitor — nothing about this request is remembered, which is what lets it be
		// cached and indexed — and a handler that minted one broke it on a page a
		// crawler will keep. The writer keeps the promise (see withholdCookies): the
		// cookie never reaches the visitor, whatever size the body turned out to be.
		// Where the response is still held it becomes a 500 naming the fault, the body
		// discarded; where the bytes went, the line names the route.
		//
		// The last chance to answer a refusal in the shape the caller asked for. huma
		// writes the refusals it makes itself — a body over the operation's bound, a
		// body that will not decode, a handler's 5xx — with its own encoder, past the
		// reach of fault.go. This is the one place that holds the finished answer, so a
		// problem document that arrived from huma is read back through the kernel's one
		// writer. A caller that named a value is untouched, as is a begun response.
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
