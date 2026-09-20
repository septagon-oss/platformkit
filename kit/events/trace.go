package events

// The tracing half of delivery. Two facts live here: the context a publisher
// leaves on the outbox row, and the span one attempt at handling an event makes.
// Both go through the global propagator and the global tracer rather than
// through a parameter, for the reason kit/telemetry gives: the publisher is a
// module's service and the delivery is a transport's callback, so a tracer
// passed down would be an argument every module already ignores.

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// tracer is this package's instrumentation scope, named for the package that
// makes the spans so a reader of a trace knows whether the relay or a handler
// wrote a line.
var tracer = otel.Tracer("github.com/septagon-oss/platformkit/kit/events")

// traceContext is the W3C trace context of the work publishing an event, as the
// two strings the envelope and the outbox columns hold. An untraced publisher —
// a periodic job, or any process with no collector configured — gets two empty
// strings, which is what the columns are for.
func traceContext(ctx context.Context) (parent, state string) {
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	return carrier["traceparent"], carrier["tracestate"]
}

// startDelivery opens the span for one attempt at one event. It is a child of
// the publisher when the event carries a context, which is the whole point of
// storing that context on the outbox row: the request that moved the state and
// the handler that reacted to it are one trace, across the database, the relay
// and the broker. The tenant of a delivery is the id the event names — this path
// has no slug to name it with, and a query per delivery to ask for one is not a
// price tracing should charge.
func startDelivery(ctx context.Context, ev Event, system string) (context.Context, trace.Span) {
	if ev.TraceParent != "" {
		ctx = otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier{
			"traceparent": ev.TraceParent, "tracestate": ev.TraceState})
	}
	attrs := []attribute.KeyValue{
		attribute.String("messaging.message.id", ev.ID.String()),
		attribute.String("messaging.destination.name", ev.Name),
		attribute.String("platformkit.tenant", ev.TenantID.String()),
	}
	if system != "" {
		attrs = append(attrs, attribute.String("messaging.system", system))
	}
	// Absent rather than zero: an adapter whose broker does not count deliveries
	// has no attempt to report, and 0 would read as a first attempt that was
	// never recorded anywhere.
	if ev.Attempt > 0 {
		attrs = append(attrs, attribute.Int("platformkit.events.attempt", ev.Attempt))
	}
	return tracer.Start(ctx, ev.Name+" deliver",
		trace.WithSpanKind(trace.SpanKindConsumer), trace.WithAttributes(attrs...))
}
