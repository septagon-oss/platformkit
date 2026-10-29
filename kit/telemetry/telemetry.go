// Package telemetry is the kernel's measurement vocabulary: the attribute keys a
// span carries, the request id every span can name, and the instruments the three
// numbers this runtime promises are made of.
//
// It holds no provider, no exporter and no sampler: those are chosen once, by the
// composition, in kit/app. A kernel package that makes a span must not be able to
// install the process's tracer or link an exporter it was not chosen for, and
// scripts/check_packages.sh refuses the day that stops being true.
//
// The two tenant keys, why the tenant is a span attribute and never a resource one,
// and what is deliberately absent are reasoned in README.md. The short forms: never
// invent a slug, never put a tenant in the resource of a process that serves many,
// and stamp the request id rather than escape it.
package telemetry

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// Scope is the instrumentation scope every span and meter in the kernel names
// itself with, so a trace backend says "PlatformKit's outbox relay" rather than
// guessing from a span name.
const Scope = "github.com/septagon-oss/platformkit/kit/telemetry"

// Tracer returns a tracer of this scope from the provider the process has
// installed at the moment of the call.
//
// It is a call and not a `var tracer = otel.Tracer(Scope)` in each package that
// opens a span, because a tracer taken from the OpenTelemetry global before any
// provider exists rebinds to the installed one exactly once: a package that held
// such a tracer would go on writing spans to the *first* provider the process
// installed. For a process that boots telemetry before anything touches it that is
// the same answer as asking each time; everywhere else the spans arrive somewhere
// the caller cannot read and nothing reports the difference — a span recorder in a
// test, or a second boot in one process, is that elsewhere.
//
// Asking costs one global lookup and one small allocation, which a transaction
// that costs a database round trip does not notice.
func Tracer() trace.Tracer { return otel.Tracer(Scope) }

// The attribute keys this runtime writes. They are constants because an
// attribute that is spelled twice is an attribute a dashboard cannot filter on,
// and because a reader has to be able to find every writer of a key by one grep.
const (
	AttrTenant    = "pkit.tenant"
	AttrTenantID  = "pkit.tenant.id"
	AttrClient    = "pkit.client"
	AttrRequestID = "pkit.request.id"
)

// Propagators is the propagation set every PlatformKit process installs: the W3C
// trace context, and the baggage beside it.
//
// It is exported because reading a trace context is a different thing from
// exporting spans. A process with no collector still receives requests that carry
// a trace parent and still leaves a context on the outbox row for a traced
// process to read, and neither is possible without these. A process that does not
// compose itself through kit/app still has to install the same set.
func Propagators() propagation.TextMapPropagator {
	return propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{})
}

// WithRequestID returns ctx carrying the request id as W3C baggage, so a span
// opened anywhere below — in kit/db, in a job, in a delivery — can name the
// request that caused it without the id being an argument.
//
// It writes a bag, not a member into whatever bag was already there, and every
// answer it gives is a bag. That is the point of the shape: baggage is a request
// header this package propagates, so by the time a router middleware calls this,
// ctx already holds the bag the caller sent, and baggage.New(m) replaces that bag.
// A version that returned ctx unchanged when it had nothing to write would leave
// the caller's own id as the only one below the router, and every span stamped
// below would name a request nobody was answered for.
//
// An id W3C will not carry is left uncarried rather than escaped; the response
// header and the request's own span still name it.
func WithRequestID(ctx context.Context, id string) context.Context {
	empty := baggage.Baggage{}
	if id == "" {
		return baggage.ContextWithBaggage(ctx, empty)
	}
	m, err := baggage.NewMember(AttrRequestID, id)
	if err != nil {
		return baggage.ContextWithBaggage(ctx, empty)
	}
	b, err := baggage.New(m)
	if err != nil {
		return baggage.ContextWithBaggage(ctx, empty)
	}
	return baggage.ContextWithBaggage(ctx, b)
}

// RequestID is the request id carried in ctx's baggage, "" when there is none.
func RequestID(ctx context.Context) string {
	return baggage.FromContext(ctx).Member(AttrRequestID).Value()
}

// SpanAttrs is what every span this kernel opens carries: the tenant, both keys,
// written only for the parts ctx actually holds, and the request id when the
// request that caused this work is known.
func SpanAttrs(ctx context.Context) []attribute.KeyValue {
	var attrs []attribute.KeyValue
	if t, ok := tenancy.FromContext(ctx); ok {
		attrs = append(attrs, attribute.String(AttrTenantID, t.ID.String()))
		if t.Slug != "" {
			attrs = append(attrs, attribute.String(AttrTenant, t.Slug))
		}
	}
	if id := RequestID(ctx); id != "" {
		attrs = append(attrs, attribute.String(AttrRequestID, id))
	}
	return attrs
}

// MetricAttrs is what a number this kernel records carries: the tenant, both
// keys, and nothing that names one request.
//
// It is SpanAttrs minus the request id, and that difference is the difference
// between a span and a time series. A span describes one request, so an attribute
// that names that request is the most useful thing it can carry; a metric
// attribute is part of the identity of a series, so a value unique per request
// makes one series per request, which is a dashboard that cannot average anything
// and a backend whose index grows with traffic. The request is not lost: the
// exemplar OpenTelemetry attaches to every measurement names the trace and span
// it came from, and that trace carries the id — which is the way a number is
// supposed to lead to an example of itself. The collector that has this kernel
// pointed at it prints both, and prints a series per class of refusal, not one
// per refusal.
func MetricAttrs(ctx context.Context) []attribute.KeyValue {
	var attrs []attribute.KeyValue
	if t, ok := tenancy.FromContext(ctx); ok {
		attrs = append(attrs, attribute.String(AttrTenantID, t.ID.String()))
		if t.Slug != "" {
			attrs = append(attrs, attribute.String(AttrTenant, t.Slug))
		}
	}
	return attrs
}

// RefusalClass is the closed set a refusal counter keys on, from the status the
// client was answered with.
//
// It is here rather than in kit/httpx because the set is a promise to whoever
// reads the number: it is closed, so a new refusal reason lands in one of these
// thirteen and never in a class somebody invented at a call site. A reason does
// not have to be invented at a call site to be missing from the table — the
// statuses kit/httpx answers and the OpenAPI validator answers both have to be
// here, which is what TestEveryRefusalThisKernelWritesHasAClassOfItsOwn checks.
// It is a function
// of the status and nothing else, because the status is what the kernel already
// decides and commits to on the response; a class read from a handler's error
// would be a second answer to a question the response already answered.
func RefusalClass(status int) string {
	switch status {
	case 400, 414, 422, 431:
		return "invalid"
	case 401:
		return "unauthenticated"
	case 403, 407:
		return "forbidden"
	case 402:
		// The plan gate: kit/httpx answers this itself (CodePlanExcludes), and an
		// operator watching the number needs to tell "this tenant's plan does not
		// cover it" from "this caller may not" — one is a commercial fact about a
		// tenant, the other is a security event.
		return "plan_excluded"
	case 404:
		return "not_found"
	case 405, 406:
		return "not_allowed"
	case 408, 504:
		return "timeout"
	case 409, 412, 428:
		return "conflict"
	case 413, 415:
		return "too_large"
	case 429:
		return "rate_limited"
	case 502, 503:
		return "unavailable"
	}
	if status >= 500 {
		return "failed"
	}
	return "unknown"
}
