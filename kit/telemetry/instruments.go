package telemetry

// The three instruments this runtime promises, and nothing else. A number a
// dashboard cannot act on is a number nobody reads, so the set is small and each
// one answers a question an operator asks during an incident: is the site slow
// and for whom, is the queue draining, and is anything being refused.
//
// A tenant dimension belongs on every one of them. That is the point of the
// runtime: one process, many tenants, and an aggregate that cannot be split by
// tenant cannot tell you which customer is having the bad afternoon. It is also
// the reason the instruments are recorded where the tenant is known rather than
// counted centrally at the edge.

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// latencyBuckets is what a request's seconds are counted against, from five
// milliseconds to a minute. The SDK's defaults stop at ten seconds, and the work
// this runtime does that is slow enough to matter — an upload, an export, a page
// that reads four tables — lives past that, so defaults would put every slow
// request in the same last bucket and answer "how slow?" with "at least ten
// seconds".
var latencyBuckets = []float64{.005, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 30, 60}

// Instruments holds the three numbers. A nil instrument is one the meter could
// not make — a name this version of the API will not accept — and every record
// below checks for it, because a measurement that cannot be made is a loss and a
// panic in a request handler is an outage, and only one of them is worth having.
type Instruments struct {
	// OperationLatency counts how long a registered operation took, in seconds,
	// named by the operation id the OpenAPI document gives the call — the same
	// name the span carries — so a slow endpoint and a slow trace agree.
	OperationLatency metric.Float64Histogram
	// OutboxLag is how long an outbox row waited between its commit and the
	// relay that published it. It is a gauge and not a histogram because the
	// question is "how far behind is the queue now", and the answer a reader
	// wants is the newest one, not a distribution of the last ten seconds.
	OutboxLag metric.Float64Gauge
	// Refusals counts the answers the kernel gave a client it would not serve,
	// by class. It counts refusals and not errors: a 500 the handler returned is
	// already in the latency histogram with an error status on its span, and what
	// nobody sees without this is the request that was refused on purpose.
	Refusals metric.Int64Counter
}

// NewInstruments makes the three against m. kit/app makes the process's meter
// provider and installs it as the global; the kernel's own meters are taken from
// the global at initialization, which OpenTelemetry binds to whatever provider is
// installed later — so a package that records a number needs no provider of its
// own and no parameter to carry one.
func NewInstruments(m metric.Meter) Instruments {
	var in Instruments
	in.OperationLatency, _ = m.Float64Histogram("pkit.http.operation.duration",
		metric.WithUnit("s"), metric.WithDescription("Seconds spent in one registered operation, by operation and tenant"),
		metric.WithExplicitBucketBoundaries(latencyBuckets...))
	in.OutboxLag, _ = m.Float64Gauge("pkit.outbox.lag",
		metric.WithUnit("s"), metric.WithDescription("Seconds an outbox row waited for the relay that published it, by tenant and event"))
	in.Refusals, _ = m.Int64Counter("pkit.http.refusals",
		metric.WithUnit("{request}"), metric.WithDescription("Requests answered with a refusal, by class and tenant"))
	return in
}

// shared is what the instrumented kernel packages record on: a meter taken from
// the global provider before any provider exists, which OpenTelemetry re-binds to
// the one that is installed afterwards. kit/app installs it, once, at the boot.
var shared = NewInstruments(otel.Meter(Scope))

// Shared is the process's three instruments.
func Shared() *Instruments { return &shared }

// ObserveOperation records one operation's duration. The attributes are the
// caller's because the caller is the one that knows the operation, and they come
// from MetricAttrs rather than SpanAttrs because a metric attribute is a time
// series' identity: the tenant belongs on the number, the request id does not.
// What a span and a number agree on is the tenant, the operation and the values;
// what only the span can carry is the thing that makes the request one.
func (i *Instruments) ObserveOperation(ctx context.Context, seconds float64, attrs ...attribute.KeyValue) {
	if i == nil || i.OperationLatency == nil {
		return
	}
	i.OperationLatency.Record(ctx, seconds, metric.WithAttributes(attrs...))
}

// ObserveOutboxLag records how long one row waited.
func (i *Instruments) ObserveOutboxLag(ctx context.Context, seconds float64, attrs ...attribute.KeyValue) {
	if i == nil || i.OutboxLag == nil {
		return
	}
	i.OutboxLag.Record(ctx, seconds, metric.WithAttributes(attrs...))
}

// AttrRefusalClass is the attribute the refusal counter's class arrives under,
// exported so a test names the same key the counter writes rather than a copy of
// its spelling.
const AttrRefusalClass = "pkit.refusal.class"

// CountRefusal records one refusal of one class.
func (i *Instruments) CountRefusal(ctx context.Context, class string, attrs ...attribute.KeyValue) {
	if i == nil || i.Refusals == nil {
		return
	}
	i.Refusals.Add(ctx, 1, metric.WithAttributes(append(attrs, attribute.String(AttrRefusalClass, class))...))
}
