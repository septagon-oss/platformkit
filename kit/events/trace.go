package events

// The tracing half of delivery. Three facts live here: the context a publisher
// leaves on the outbox row, the spans one publication and one delivery of an
// event make, and the trace id every audit record of this package can be joined
// by. All three go
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

// rowContext returns the context one outbox row's own propagated members describe:
// the trace the row stores, when it stores one, and the correlation value the row
// stores, which for a job's event is none at all.
//
// The caller's correlation is emptied *before* the row's carrier is extracted, and
// that order is the reason this function exists. A reader cannot clear it: an empty
// `baggage` member makes propagation.Baggage.Extract hand back its parent, so the
// row's members, extracted however conditionally, leave standing whatever bag the
// caller's context already holds. A pass runs on the context of whoever asked for it,
// and when that is a request, a row that stores no member would otherwise be published
// and delivered under it — the event owner's publication joined to a request that
// caused nothing, another customer's request whenever the relay was not its own.
// The correlation a row's spans name is therefore its own member or nothing.
//
// The trace members keep the caller's context as their parent when the row stores
// none, because TraceContext.Extract returns its parent unchanged for a carrier with
// no valid traceparent: a job's event in a process that exports nothing hangs off the
// pass span, which is the trace it belongs to.
func rowContext(ctx context.Context, ev Event) context.Context {
	ctx = telemetry.WithRequestID(ctx, "")
	if ev.TraceParent != "" || ev.Baggage != "" {
		ctx = otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier{
			"traceparent": ev.TraceParent, "tracestate": ev.TraceState, "baggage": ev.Baggage})
	}
	return ctx
}

// startPublication opens the span for one event leaving the outbox: the moment
// the row's payload is handed to a transport.
//
// It is a span per row, and the batch span beside it stays. The two answer two
// questions, and one span cannot answer both: "is the queue draining, and how
// long was a pass" is a fact about a batch of a hundred rows, while "whose event
// went out, under which request" is a fact about one of them. A batch is read
// across every tenant, so the one tenant a pass span could name would name none
// of the others; this span names the tenant of the row it published and nothing
// else's.
//
// Its parent is the trace stored on the row, extracted the way startDelivery
// extracts it, so a hundred-row burst puts a hundred spans on the hundred traces
// that caused them and leaves the worker's own trace the one pass span it was; a
// row that stores no trace — a job's event in a process that exports nothing —
// hangs off the pass span, which is the trace it belongs to.
// The request id comes from the same place — the correlation member the publisher
// left, which is the only way a relay learns it — and a row with no member names
// no request and keeps its tenant, which is the ordinary case for a job's event:
// rowContext is what makes an absent member mean no request rather than whatever
// request the pass happens to be running under.
//
// The tenant is named by id alone, as in startDelivery: a relay holds the UUID its
// row carries and the slug would cost a query per row.
func startPublication(ctx context.Context, ev Event) (context.Context, trace.Span) {
	ctx = rowContext(ctx, ev)
	attrs := []attribute.KeyValue{
		attribute.String(telemetry.AttrTenantID, ev.TenantID.String()),
		attribute.String("messaging.operation.name", "publish"),
		attribute.String("messaging.source.name", ev.Name),
		attribute.String("messaging.message.id", ev.ID.String()),
	}
	// The id is read back off the extracted context by the one accessor that owns
	// that carrier, the way every span below a delivery reads it. The tenant is
	// written from the row rather than read out of ctx by telemetry.SpanAttrs: the
	// context a pass runs on is a system transaction's and names no tenant, and a
	// publication whose tenant came from anywhere but its row would name the wrong
	// customer for someone else's event.
	if id := telemetry.RequestID(ctx); id != "" {
		attrs = append(attrs, attribute.String(telemetry.AttrRequestID, id))
	}
	return telemetry.Tracer().Start(ctx, ev.Name+" publish",
		trace.WithSpanKind(trace.SpanKindProducer), trace.WithAttributes(attrs...))
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
// migrations/000041) is the ordinary case and gives the trace alone — and no request,
// not the request the delivery's caller is standing in, which rowContext drops before
// the row's members are read.
//
// The tenant is named by id and not by slug, because this path has only the id: a
// job that lists tenants or a request that resolved a host has both names, and a
// delivery holds the UUID its event carries. Getting the slug would be a query per
// delivery, which is not a price tracing should charge. kit/telemetry's comment
// names the two keys and which spans write which.
func startDelivery(ctx context.Context, ev Event) (context.Context, trace.Span) {
	ctx = rowContext(ctx, ev)
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
