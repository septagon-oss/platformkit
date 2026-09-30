// Package telemetry is the kernel's measurement vocabulary: the attribute keys a
// span carries, the request id every span can name, and the instruments the
// three numbers this runtime promises are made of.
//
// It holds no provider, no exporter and no sampler. Those are chosen once, by
// the composition, in kit/app — which is the package the brief of this runtime
// names as the home of one TracerProvider and one MeterProvider. The split is
// the reason this package exists: a kernel package that makes a span must not be
// able to install the process's tracer, and it must not link an exporter it was
// not chosen for. kit/httpx, kit/db, kit/events and kit/jobs import this one and
// the OpenTelemetry API beside it; scripts/check_packages.sh is what refuses the
// day that stops being true.
//
// # The two tenant keys
//
// pkit.tenant is the slug — the name a person reading a trace recognises — and
// pkit.tenant.id is the UUID. Two keys, because a delivery has only the id its
// event names and a request has both: one key carrying a slug on one span and a
// UUID on the other cannot be filtered on by anybody who does not already know
// which span they are looking at, which is the whole use of an attribute. A span
// writes the slug only where the slug was learned, and never an invented one: a
// host the resolver does not know has neither key, which is the honest answer.
//
// # Why the tenant is a span attribute and never a resource attribute
//
// A resource describes the *process*. This kernel is one process serving many
// tenants (0028's shared-instance mode resolves a tenant per request, from the
// host), so a tenant in the resource would either be a lie for every request but
// one or a provider per tenant, which is the unpickable singleton the pillar
// contract refuses. pkit.client is the one exception, and it is a deployment's
// own fact rather than a request's: an installation that serves exactly one client
// may name it in telemetry.client and get it on every span of that process; a
// shared installation names nothing and reads the client off the tenant.
//
// # The request id travels as W3C Baggage
//
// Every span this kernel opens is a child of the span the request arrived with —
// the SQL transaction, the tenant's share of a job, the publish — so the request
// id is already one hop away on any trace viewer. It is *stamped* as well, on the
// spans this kernel opens itself, because a job run has no request span to sit
// under and an operator joins a log line to a trace by quoting the id the response
// header carried. Baggage is the standard carrier for a correlation
// value that is not a trace parent, it rides the composite propagator this package
// installs, and nothing here puts a header, a query value or a body in it.
//
// One boundary has a request span to sit under and no way to inherit from it: a
// delivery happens in another process, so the member is stored on the outbox row
// beside the two W3C trace members (migrations/000028 and 000029) and read back
// where the delivery's span is opened. A delivery span with a parent and no request
// id on it is the half-measure this comment used to be.
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
// provider exists rebinds to the installed one exactly once — otel's
// internal/global states it in those words, "It is guaranteed by the caller that
// this happens only once". A kernel package that held such a tracer in a variable
// would go on writing its spans to the *first* provider the process installed,
// whatever the composition installs afterwards. For the process that boots
// telemetry once before anything else touches it, that is the same answer as
// asking each time; everywhere else it is silent and wrong: the spans the caller
// opened arrive somewhere it cannot read, and nothing reports the difference. The
// reader that installs a provider to see what this kernel emits is the common case
// for the difference — a span recorder in a test, or a second boot in one process.
//
// Asking costs one lookup of the global and one small allocation beside the span
// itself, which a transaction that costs a database round trip does not notice.
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
// An id W3C will not carry (baggage forbids "=" and "," in a value, and
// kit/httpx accepts any printable ASCII a client sends) is left out rather than
// escaped: the response header and the request's own span still name it, and a
// correlation value silently rewritten in transit would name a request nobody
// asked about.
func WithRequestID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	m, err := baggage.NewMember(AttrRequestID, id)
	if err != nil {
		return ctx
	}
	b, err := baggage.New(m)
	if err != nil {
		return ctx
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
