package httpx

// This file is the request's share of the kernel's measurement: the span the
// router opens, the name and the attributes this router is the only place that
// knows them, and the two numbers a request is worth — how long the operation
// took, and whether it was refused.
//
// The span itself is opened by otelhttp, outermost on the router, so a static
// file, a probe and a 404 that never reached a handler are all inside a span; what
// lives here is what otelhttp could not know. It runs the span before routing, and
// before routing there is no operation, no tenant and no request id — so the name
// and the attributes are stamped where each is learned, in the middleware that
// learns it, rather than assembled at the end from a context that by then holds a
// mixture of resolved and unresolved.

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"

	"github.com/septagon-oss/platformkit/kit/telemetry"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// tracing is the router middleware that opens the request's span. It is the first thing on the
// router for the reason in its comment: everything the router serves is inside it.
//
// The empty operation name is honest rather than tidy. otelhttp names a server span from its
// HTTP semantic-convention formatter, and a name passed here would reach a formatter that never
// sees this router's route, because chi keeps the pattern to itself: a request that matches
// nothing keeps the method as its name, and one that reached an operation is renamed by traced.
//
// The no-op meter is this runtime's promise, not otelhttp's: the same middleware registers HTTP
// server instruments, and left to the global they would be live the moment this runtime started
// measuring for a reason unrelated to this router. What this repository promises is one latency
// histogram per operation, one refusal counter and one queue gauge, with attributes chosen
// beside them.
func tracing(next http.Handler) http.Handler {
	return otelhttp.NewHandler(next, "", otelhttp.WithMeterProvider(metricnoop.NewMeterProvider()))
}

// spanAttr records one fact on the span the router opened for this request.
//
// It is a call where the fact is learned rather than a list assembled in one
// middleware, because the tenant resolves in one middleware and the request id in
// another, and a request that resolved neither must not carry an attribute that
// guesses at them: an unresolved host is not a tenant, and an id invented for a
// tenant nobody resolved would read in a trace as a site that exists. A request
// outside a recording span — a build with no collector, or a sampled-out trace —
// costs a method call and no allocation.
func spanAttr(ctx context.Context, key, value string) {
	if value == "" {
		return
	}
	trace.SpanFromContext(ctx).SetAttributes(attribute.String(key, value))
}

// traced names the request's span after the operation that answered it, says which route that
// operation is, and records the operation's latency.
//
// It runs first among the middlewares that know the operation — second in the chain, after
// tenant, the one thing in it that has to see a request before an operation exists — because
// it answers a question otelhttp could not: the span has to exist before routing, or a request
// that matches nothing has no span at all, and before routing there is no operation. chi
// leaves no pattern on the request the way net/http's ServeMux does, so the operation is the
// only place this router's route is written down, and its id is the name the OpenAPI document
// already gives the call.
//
// The number itself is recorded by tenant.go, which is where the tenant is known.
func (a *API) traced(ctx huma.Context, next func(huma.Context)) {
	op := ctx.Operation()
	span := trace.SpanFromContext(ctx.Context())
	if op != nil && op.OperationID != "" && span.IsRecording() {
		span.SetName(op.OperationID)
		span.SetAttributes(attribute.String("http.route", op.Path))
	}
	next(ctx)
}

// observeOperation records one operation's latency, named by the operation id the
// OpenAPI document gives the call and by the tenant the host resolved.
//
// The caller hands over the context it resolved that tenant on rather than the
// attributes: the tenant is read from the context by telemetry.MetricAttrs, which
// is the one place that decides what a number may carry. Passing the attributes
// here instead would put the cardinality decision in every call site.
func observeOperation(ctx context.Context, op *huma.Operation, seconds float64) {
	if op == nil || op.OperationID == "" {
		return
	}
	telemetry.Shared().ObserveOperation(ctx, seconds,
		append(telemetry.MetricAttrs(ctx),
			attribute.String("pkit.operation", op.OperationID),
			attribute.String("http.request.method", op.Method))...)
}

// countRefusal records that this request was answered with a refusal of this class.
//
// Its two callers count disjoint answers: fail, for the refusal the root router answers
// before the mounted API exists and so before any response buffer does — the address gate and
// the static tree — and respond, for every answer of the mounted API, counted once from the
// status the client was finally answered with. The split is by *where the request was
// answered*, not by who wrote the body: chi's 404 and 405 are written by a.fail and counted by
// respond, because the mount matched and the buffered chain ran before chi decided nothing was
// there.
//
// The second caller is why the guards inside the huma chain (refuse) count nothing themselves:
// the router answers a great deal without any kernel writer in sight, and the buffer replaces
// answers (a held 200 becomes a 500 when the commit fails), so a counter that noticed only the
// answers it wrote itself would both read zero and double count.
func countRefusal(ctx context.Context, status int) {
	// MetricAttrs and not SpanAttrs: the one key that difference drops is the request id,
	// and an attribute unique to one request would answer "how many of this class, for
	// whom" with one time series per refusal.
	telemetry.Shared().CountRefusal(ctx, telemetry.RefusalClass(status), telemetry.MetricAttrs(ctx)...)
}

// answerNote is where the tenant a request resolved is carried outward to the
// middleware that counts the answer, which sits outside the middleware that
// resolved it and holds a request context that predates the resolution.
//
// The alternative was to count where the tenant is known and accept counting an
// answer that may still be replaced by the buffer — a refusal counted for a request
// that was finally answered 200, which is the number this counter exists not to
// write. So the tenant travels, one pointer beside the buffer it shares a lifetime
// with, and the count happens once, after the response is final.
//
// Nothing guards it: the write happens inside the call stack of the read, on one
// goroutine, and the read happens after that stack has returned.
type answerNote struct {
	tenant tenancy.Tenant
}

// noteAnswer records the resolved tenant on this request's note, where respond is
// holding one. It does nothing on a request that never reached the mounted API,
// which is the case fail counts without a tenant and gets right in doing so.
func noteAnswer(ctx context.Context, t tenancy.Tenant) {
	if n, ok := ctx.Value(answerNoteKey{}).(*answerNote); ok {
		n.tenant = t
	}
}

// context is the context a refusal of this request is counted on: the request's
// own, plus the tenant the host resolved, when one was resolved. Same keys, same
// rules as every other number — this is telemetry.SpanAttrs reading a context that
// now holds what the span already carries.
func (n *answerNote) context(ctx context.Context) context.Context {
	if n == nil || n.tenant.ID == uuid.Nil {
		return ctx
	}
	return tenancy.WithTenant(ctx, n.tenant)
}
