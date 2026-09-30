package events

// The tracing half of delivery. Three facts live here: the context a publisher
// leaves on the outbox row, the span one delivery of an event makes, and the
// trace id every audit record of this package can be joined by. All three go
// through the global propagator and the global tracer rather than through a
// parameter, for the reason kit/telemetry gives: the publisher is a module's
// service and a delivery is a transport's callback, so a tracer passed down would
// be an argument every module already ignores. The tracer is taken from
// telemetry.Tracer at each span rather than held in a package variable, which is
// why a span this package opens arrives on the provider the process installed and
// not on the first one it ever installed.

import (
	"context"
	"errors"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	w3c "github.com/septagon-oss/platformkit/kit/trace"

	"github.com/septagon-oss/platformkit/kit/telemetry"
)

// The three members the row holds are the two W3C distributed-tracing members and
// the correlation member that travels beside them: traceparent and tracestate say
// which span this event happened under, and the baggage member carries the request
// id the router put on the publisher's context. An untraced
// publisher — a periodic job, or any process with no collector configured — leaves all
// three absent, and the write stores an absent member as NULL (nilIfEmpty), which is
// what the columns are for and what a query asks with IS NULL.
//
// The two trace members are read from two carriers, in this order:
//
//   - the context the OpenTelemetry propagator injects, which is the trace a
//     collector holds when the process installed a provider, because it names the
//     span that is open on this request rather than a context this process minted
//     for itself;
//   - kit/trace's carried context, which a request always leaves behind — parsed
//     from the caller's own traceparent, or opened from the request id — and which is
//     the same W3C format. A process that installed no provider, which every test
//     binary and every deployment that points at no collector is, would otherwise
//     write an event that names no trace at all.
//
// The order matters once and only: when both hold a value the process has a provider,
// and the injected value is the one the spans this process exports are members of, so
// picking the carried value would put the event under a trace no span belongs to.
// The correlation member comes from the propagator alone, because kit/trace carries no
// correlation value — that is what the baggage column is for.
func carriedContext(ctx context.Context) (parent, state, correlation string) {
	parent, state, correlation = traceContext(ctx)
	if parent == "" {
		if tc, ok := w3c.From(ctx); ok {
			parent, state = tc.Parent(), tc.TraceState
		}
	}
	return parent, state, correlation
}

// traceContext injects the publisher's context into a carrier and reads the three
// members back. It reads nothing that the OpenTelemetry global does not hold: with no
// provider installed the tracer hands back a span with no context, so an untraced
// publisher yields three empty strings and the row stores three NULLs.
func traceContext(ctx context.Context) (parent, state, correlation string) {
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	return carrier["traceparent"], carrier["tracestate"], carrier["baggage"]
}

// startDelivery opens the span for one delivery of one event. It is a child of
// the publisher when the event carries a context, which is the whole point of
// storing that context on the outbox row: the request that moved the state and the
// handler that reacted to it are one trace, across the database, the relay and the
// broker.
//
// The correlation member is extracted with the trace members, and for the same
// reason: the brief asks every boundary span to name the request that caused it, and
// this boundary is the one where the request is in another process. A delivery has no
// request span to sit under, so the id is read out of the baggage the publisher left
// and lands on this span and on every span the handler opens below it. An event with
// only a trace and no baggage (a job that published it, or a row written before
// migrations/000029) is the ordinary case and gives the trace alone.
//
// The tenant is named by id and not by slug, because this path has only the id: a
// job that lists tenants or a request that resolved a host has both names, and a
// delivery holds the UUID its event carries. Getting the slug would be a query per
// delivery, which is not a price tracing should charge. kit/telemetry's comment
// names the two keys and which spans write which.
func startDelivery(ctx context.Context, ev Event) (context.Context, trace.Span) {
	if ev.TraceParent != "" || ev.Baggage != "" {
		ctx = otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier{
			"traceparent": ev.TraceParent, "tracestate": ev.TraceState, "baggage": ev.Baggage})
	}
	attrs := append(telemetry.SpanAttrs(ctx),
		// Which broker carried this is the adapter's fact, and this package does not
		// know it: memory and JetStream are both a Transport here. So the span names
		// the message and the source and says nothing about the system, rather than
		// guessing a value a reader would filter on.
		attribute.String("messaging.operation.name", "process"),
		attribute.String("messaging.source.name", ev.Name),
		attribute.String("messaging.message.id", ev.ID.String()))
	return telemetry.Tracer().Start(ctx, ev.Name+" deliver",
		trace.WithSpanKind(trace.SpanKindConsumer), trace.WithAttributes(attrs...))
}

// endSpan is how a delivery's failure reaches its span: the error itself, a status,
// and the same nil-safety the rest of the kernel's spans use, so a handler that
// failed on the way to the database still leaves a red span rather than no span.
func endSpan(span trace.Span, err error) {
	if err != nil && !errors.Is(err, context.Canceled) {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()
}
