package httpx

// respond.go is the outermost middleware: it turns a panic into a problem
// document and refuses to let a public route's cookie reach the client.

import (
	"context"
	"encoding/json"
	"net/http"
	"runtime/debug"

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
		// huma.Context, so the claim it took travels outward to the one defer that
		// knows what the client was finally given.
		r = r.WithContext(withClaim(r.Context()))
		// The keyed write's own slot, beside the buffer it shares a lifetime with:
		// the guard that takes the claim runs below here and holds a huma context, so
		// the claim travels outward the same way the resolved tenant does.
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
				a.complete(r.Context(), n, b.status, b.Header().Get("Content-Type"), b.body.Bytes(), held)
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
func writeProblem(w http.ResponseWriter, status int, id, detail string) {
	p := problem.New(status, detail)
	if id != "" {
		p.Instance = "urn:request:" + id
	}
	w.Header().Set("Content-Type", problem.ContentType)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(p)
}
