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
