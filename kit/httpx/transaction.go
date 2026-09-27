package httpx

// transaction.go opens the request transaction, and commits or rolls it back on
// the status the handler decided. A response nobody decided a status for is not a
// commit.

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// transaction gives the request a transaction it has not opened yet, and ends
// it once the response is decided: commit under a status below 400, roll back
// otherwise, roll back on the way out of a panic.
//
// A request that resolved to no tenant — only a public one reaches here — gets
// none, because db has no tenant to scope one to. A request that resolved to
// one but never queries opens nothing either, which is why a liveness probe
// addressed to a tenant host still answers while the database is down.
func (a *API) transaction(ctx huma.Context, next func(huma.Context)) {
	if _, ok := tenancy.FromContext(ctx.Context()); !ok {
		next(ctx)
		return
	}
	rctx, p, err := db.Lazy(ctx.Context(), a.opts.Conn, a.lazy)
	if err != nil {
		a.rlog(ctx.Context()).ErrorContext(ctx.Context(), "httpx: no transaction for this request", "error", err)
		a.refuse(ctx, http.StatusInternalServerError, "")
		return
	}
	// Close is idempotent, so this covers the panic path and nothing else.
	defer func() { _ = p.Close(false) }()

	// The connection travels with the transaction: a control-plane route needs
	// both it and a token to open a transaction of its own. See ConnFrom.
	rctx = WithConn(rctx, a.opts.Conn)
	inner := huma.WithContext(ctx, context.WithValue(rctx, txKey{}, p))
	next(inner)

	if openErr := p.Err(); openErr != nil {
		// The handler asked for the transaction and did not get one. Its own
		// error is already the response; this is the cause behind it.
		a.rlog(ctx.Context()).ErrorContext(ctx.Context(), "httpx: could not open the request transaction",
			"method", ctx.Method(), "path", ctx.URL().Path, "error", openErr)
	}

	// A status of zero is not a success. huma leaves it at zero on the
	// streaming path, where the handler has already written the answer itself,
	// and it is also what a handler that returned without deciding anything
	// leaves behind. The two are told apart by whether the response has begun:
	// if bytes are on the wire the answer is 200 whatever this middleware
	// thinks, so commit and say so; if nothing has been sent, nobody decided,
	// and a transaction nobody decided about must not commit.
	keep, undecided := statusOf(ctx, a, inner.Status())
	err = p.Close(keep)
	b, buffered := bufferFrom(ctx.Context())
	switch {
	case err == nil:
		if undecided && buffered && b.reset() {
			a.refuse(ctx, http.StatusInternalServerError, "")
		}
	case hungUp(ctx.Context(), err):
		// Nobody is waiting for this answer and nothing was written. See hungUp.
		a.rlog(ctx.Context()).InfoContext(ctx.Context(), "httpx: the client hung up before the request transaction ended",
			"method", ctx.Method(), "path", ctx.URL().Path)
	case buffered && b.reset():
		// Nothing has been sent yet, so the response can still tell the truth.
		a.rlog(ctx.Context()).ErrorContext(ctx.Context(), "httpx: the request transaction did not commit",
			"method", ctx.Method(), "path", ctx.URL().Path, "error", err)
		a.refuse(ctx, http.StatusInternalServerError, "")
	default:
		a.rlog(ctx.Context()).ErrorContext(ctx.Context(), "httpx: request transaction failed after the response was written",
			"method", ctx.Method(), "path", ctx.URL().Path, "error", err)
	}
}

// hungUp reports that the transaction ended the way a client going away ends
// one, rather than the way a database going away does.
//
// A browser that closes the tab cancels the request context. database/sql
// notices before this middleware does and rolls the transaction back itself, so
// the commit asked for a moment later comes back "sql: transaction has already
// been committed or rolled back", and the settings re-read before it fails on
// the same cancelled context. Both read as faults and neither is one: nothing
// was written, and there is nobody left to tell.
//
// It matters because the alternative was an ERROR line per abandoned request.
// An alert that fires on ordinary browsing is an alert people turn off, and the
// line it buries — the database is unreachable and requests are not committing —
// is the one worth waking somebody for.
func hungUp(ctx context.Context, err error) bool {
	return ctx.Err() != nil || errors.Is(err, sql.ErrTxDone) || errors.Is(err, context.Canceled)
}

// statusOf decides whether the request's transaction commits, and whether the
// response has to be replaced because nothing decided it. See the caller.
func statusOf(ctx huma.Context, a *API, status int) (keep, undecided bool) {
	if status != 0 {
		return status < http.StatusBadRequest, false
	}
	b, ok := bufferFrom(ctx.Context())
	if ok && b.begun() {
		a.rlog(ctx.Context()).WarnContext(ctx.Context(), "httpx: the response was streamed without a status; committing as 200",
			"method", ctx.Method(), "path", ctx.URL().Path)
		return true, false
	}
	a.rlog(ctx.Context()).ErrorContext(ctx.Context(), "httpx: the handler decided no status; rolling back",
		"method", ctx.Method(), "path", ctx.URL().Path)
	return false, true
}
