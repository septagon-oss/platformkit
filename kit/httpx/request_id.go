package httpx

// request_id.go gives every request an id, answers it in the response header and
// in the problem body's instance, and hands the logger that names it.

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/request"
	"github.com/septagon-oss/platformkit/kit/telemetry"
	"github.com/septagon-oss/platformkit/kit/trace"
)

// RequestIDHeader is the header a request id arrives in and leaves in.
const RequestIDHeader = "X-Request-ID"

// maxRequestID bounds an id a client supplied. An id is a correlation handle,
// not a payload.
const maxRequestID = 64

// requestIDFrom returns the id of the request ctx belongs to, or "" outside a
// request. It is the string the caller saw in the response header and in the
// problem body's instance; every caller of it is in this package, which is why
// it is not exported. The value itself belongs to kit/request, which is why this
// reads it rather than keeping a second copy under a second key: the id an event
// carries and the id the caller was answered with have to be one string.
func requestIDFrom(ctx context.Context) string {
	r, _ := request.From(ctx)
	return r.ID
}

// RequestID returns the id of the request ctx belongs to, and "" for a context that
// carries no request. It is the same string the response header answers in, the one the
// log line carries and the one the problem body's instance URN is made of — so a page
// that shows a reference, a body a monitor parses and the line an operator greps name one
// request rather than three.
//
// It is exported because a refusal written by a *handler* is answered with a page by
// ui/page rather than by this package, and the person reading that page is owed the same
// reference the kernel's own guards have always put on it. The id is not this package's to
// keep: kit/request owns the value, and every answer a request gets reads it from there.
func RequestID(ctx context.Context) string { return requestIDFrom(ctx) }

// requestID gives every request an id: the caller's, when they sent one worth
// keeping, so a trace that starts at a proxy stays one trace; otherwise a fresh
// UUID. It is echoed in the response header, so a caller who sent none can
// still quote it.
//
// It opens the distributed tracing context in the same breath, because the two
// identify one thing and a request that had one and not the other would be a
// log line no event could be joined to. Three sources, in this order: a caller's
// own traceparent wins — that is what makes a trace cross a boundary; failing
// that the trace id is the request id when the request id is hex, so the three
// identifiers of one call (X-Request-ID, the log's request_id, the event's
// traceparent) are one string; failing that — a proxy's opaque handle, which
// `givenID` accepts and W3C cannot use — this process opens a trace of its own,
// because a request that was accepted, answered and wrote an event has to leave
// a trace whatever the caller chose to call it by. Its trace then joins to the
// log line and the event through the request id, which all three carry, rather
// than by being the same string as it. kit/trace refuses a header it cannot
// parse rather than trusting it; refusing an id it cannot use is the same
// judgement, and is not a reason to write an event that names no trace.
func (a *API) requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := givenID(r.Header.Get(RequestIDHeader))
		if id == "" {
			id = uuid.NewString()
		}
		ctx := r.Context()
		tc, ok := trace.Parse(r.Header.Get(trace.ParentHeader), r.Header.Get(trace.StateHeader))
		if !ok {
			tc, ok = trace.FromRequestID(id)
		}
		if !ok {
			tc = trace.New()
		}
		ctx = trace.With(ctx, tc)
		// The call's three facts are recorded once, here, where all three are
		// known: the id this call will be answered with, the address of the
		// connection it arrived on, and the trace it opened. Everything that
		// outlives the call — the outbox row, and through it the audit trail —
		// reads them back from kit/request.
		me := request.Context{ID: id, ClientAddr: ClientAddr(r)}
		if tc, ok := trace.From(ctx); ok {
			me.Trace = tc
		}
		w.Header().Set(RequestIDHeader, id)
		// kit/request is the id's one owner: the line below is what every reader
		// of this call — the problem body's instance, the log line, the outbox row
		// and through it the audit trail — reads it from. Two more things happen to
		// the id here, because a request is answered by more than the process that
		// received it: it goes in the baggage, so a span opened below — in kit/db,
		// in a job the request started — can name the request that caused it
		// without the id being an argument through four signatures; and it goes on
		// the span the router already opened, which is stamped here rather than
		// carried, so a log line and a trace can be joined by quoting the string the
		// caller saw in the response header.
		//
		// The baggage and the span attribute ride beside kit/trace rather than
		// instead of it: that package carries a caller's W3C trace context to the
		// outbox row and the envelope in a process that installed no provider, and
		// this is the baggage and the span attribute, which it does not hold.
		// kit/events writes both channels onto one row.
		ctx = request.With(ctx, me)
		ctx = telemetry.WithRequestID(ctx, id)
		spanAttr(ctx, telemetry.AttrRequestID, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// givenID accepts a client's id only when it is short and printable ASCII. An
// id reaches a log line and a response header, so a newline or a kilobyte in it
// is a forged log entry or a wasted response.
func givenID(s string) string {
	if s == "" || len(s) > maxRequestID {
		return ""
	}
	for _, r := range s {
		if r < '!' || r > '~' {
			return ""
		}
	}
	return s
}

// stampRequestID is the response transformer that puts the request id into
// every problem body, as RFC 9457's instance. It lives here rather than in
// kit/problem because kit/problem knows nothing about requests: the shape is
// one package's business, the identifier is another's.
func stampRequestID(ctx huma.Context, _ string, v any) (any, error) {
	if p, ok := v.(*problem.Problem); ok && p.Instance == "" {
		if id := requestIDFrom(ctx.Context()); id != "" {
			p.Instance = "urn:request:" + id
		}
	}
	return v, nil
}

// rlog is the logger carrying this request's id, so the log line and the
// problem body the caller quotes name the same request.
func (a *API) rlog(ctx context.Context) *slog.Logger {
	if id := requestIDFrom(ctx); id != "" {
		return a.log.With("request_id", id)
	}
	return a.log
}
