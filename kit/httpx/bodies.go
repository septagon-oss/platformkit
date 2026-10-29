package httpx

// bodies.go bounds what a request may send before a handler reads any of it.

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"

	"github.com/septagon-oss/platformkit/kit/db"
)

// bodies is everything this application does about a request body before anybody reads
// one, and it is last in the chain because nothing above it reads one: huma decodes a
// schema'd body after the middlewares, and the one route that reads its own does it here.
//
// The ceiling is the half of "a greedy client cannot cost this server anything" the read
// timeout does not cover — that bounds how long a body may take, this how much of it there
// may be — and it is the operation's own (MaxBodyBytes, or Options.MaxUpload plus an
// envelope for the route that streams). http.MaxBytesReader rather than a reader of this
// package's own: it tells the server the request was too large, so the connection closes.
//
// For the streaming route alone the guards' transaction is committed here, before the
// handler: waiting must not hold it, and the handler's first query opens a second one.
func (a *API) bodies(ctx huma.Context, next func(huma.Context)) {
	_, streams := ctx.Operation().Extensions[StreamedBodyExtension]
	_, streamsResponse := ctx.Operation().Extensions[StreamedResponseExtension]
	// Either half of "the work is waiting": the route that reads its request for
	// as long as the client cares to trickle, and the route that keeps the response
	// open for as long as the caller cares to listen. Both hold a connection past the
	// point where the guards' queries are done, and neither has any use for a
	// snapshot of the rows that decided to admit it.
	waits := streams || streamsResponse
	// The request carry put on the context, which is the one a handler that
	// reads its own body reads. It is deliberately not the one huma decodes a
	// schema'd body from — huma copies the request on every WithContext, and
	// that body is huma's business and bounded by op.MaxBodyBytes. This is the
	// other kind, which nothing bounded.
	//
	// A request that brought no body is answered http.NoBody, and a reader at EOF
	// needs no ceiling: the limit exists to bound the bytes a caller may push, and
	// there are none to push. Wrapping it anyway replaced the one reader every later
	// layer shares with one nobody read and everybody had to close, on every GET this
	// server answers.
	if r, ok := RequestFrom(ctx.Context()); ok && mayCarryBytes(r) {
		limit := int64(MaxBodyBytes)
		if streams {
			limit = max(a.opts.MaxUpload, MaxBodyBytes) + multipartEnvelope
		}
		// The response writer is what lets http.MaxBytesReader tell the server
		// the request was too large, so the connection is closed rather than
		// left half-read with a response the client never sees the start of.
		_, w := humachi.Unwrap(ctx)
		r.Body = http.MaxBytesReader(w, r.Body, limit)
	}
	if p, ok := ctx.Context().Value(txKey{}).(*db.Pending); ok && waits {
		if err := p.Commit(); err != nil {
			a.rlog(ctx.Context()).ErrorContext(ctx.Context(), "httpx: the guard transaction did not commit",
				"method", ctx.Method(), "path", ctx.URL().Path, "error", err)
			a.refuse(ctx, http.StatusInternalServerError, "")
			return
		}
	}
	next(ctx)
}

// mayCarryBytes is whether this request brought a body that could yield anything.
// net/http hands http.NoBody for a request with no body, and http.NoBody and a nil
// reader are the two shapes of "there is nothing here": both answer EOF before a
// byte, so no ceiling can be crossed and nothing is refused by reading them.
func mayCarryBytes(r *http.Request) bool {
	return r.Body != nil && r.Body != http.NoBody
}
