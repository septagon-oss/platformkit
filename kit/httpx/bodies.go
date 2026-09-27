package httpx

// bodies.go bounds what a request may send before a handler reads any of it.

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"

	"github.com/septagon-oss/platformkit/kit/db"
)

// bodies is everything this application does about a request body before
// anybody reads one, and it is last in the chain because nothing above it reads
// one: huma decodes a schema'd body after the middlewares, and the one route
// that reads its own does it in the handler.
//
// It does two things.
//
// The ceiling is the half of "a slow or greedy client cannot cost this server
// anything" that kit/app's read timeout does not cover: the timeout bounds how
// long a body may take to arrive and this bounds how much of it there may be,
// and a client fast enough to beat the timeout still needs one. It is the
// operation's own — MaxBodyBytes, or Options.MaxUpload plus an envelope for the
// route that streams. http.MaxBytesReader and not a reader of this package's
// own, because it does the thing a hand-written limiter cannot: it tells the
// server the request was too large, so the connection is closed rather than
// left half-read with a response the client will never see the start of.
//
// The other thing is for the streaming route alone: the transaction the guards
// above opened is committed here, before the handler. Resolving the tenant,
// recognising the caller and checking the permission are all queries, so an
// authenticated request is holding a transaction by the time a handler is
// entered. That is right for a route whose work is a query and wrong for one
// whose work is waiting — a review trickled an upload in at a byte a second and
// pinned a connection for as long as it liked, and sixteen of those is a replica
// that serves nobody. db.Pending is built for it: Close leaves it able to open
// again, so the handler's first query opens a second transaction, after the
// bytes. That the guard and the write are two transactions is a real difference
// and it is the right one — the bytes are written outside any transaction
// anyway, and an authorization already decided is not something the row's
// transaction needs to keep a snapshot for.
func (a *API) bodies(ctx huma.Context, next func(huma.Context)) {
	_, streams := ctx.Operation().Extensions[StreamedBodyExtension]
	// The request carry put on the context, which is the one a handler that
	// reads its own body reads. It is deliberately not the one huma decodes a
	// schema'd body from — huma copies the request on every WithContext, and
	// that body is huma's business and bounded by op.MaxBodyBytes. This is the
	// other kind, which nothing bounded.
	if r, ok := RequestFrom(ctx.Context()); ok && r.Body != nil {
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
	if p, ok := ctx.Context().Value(txKey{}).(*db.Pending); ok && streams {
		if err := p.Close(true); err != nil {
			a.rlog(ctx.Context()).ErrorContext(ctx.Context(), "httpx: the guard transaction did not commit",
				"method", ctx.Method(), "path", ctx.URL().Path, "error", err)
			a.refuse(ctx, http.StatusInternalServerError, "")
			return
		}
	}
	next(ctx)
}
