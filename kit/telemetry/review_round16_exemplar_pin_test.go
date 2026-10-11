package telemetry_test

// Round 16's pin. Three documents say the same thing: the request id is left off a
// metric on purpose because "the exemplar OpenTelemetry attaches to every
// measurement names the trace and span it came from" (kit/telemetry/telemetry.go,
// kit/httpx/tracing_test.go's cardinality guard, CHANGELOG). That sentence is the
// whole justification for dropping the one key that leads from a number to a
// request, and no case read an exemplar back before this one: TestANumberCarriesNothingThatNamesOneRequest
// asserts the key is absent from the datapoint and stops there. If the SDK ever
// stops attaching an exemplar by default, or a reader is added that drops them, the
// number stops being answerable ("which request was refused?") with no test saying
// so. This case reads the exemplar off all three instruments and names the span that
// produced it, so the claim is the artifact's and not a comment's.

import (
	"context"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/telemetry"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func TestEveryMeasurementCarriesAnExemplarOfTheSpanItCameFrom(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	tracer := sdktrace.NewTracerProvider().Tracer("review-round-16")
	ctx, span := tracer.Start(context.Background(), "the request review 16 imagined")
	defer span.End()
	if !span.SpanContext().IsSampled() {
		t.Fatalf("the fixture's span is not sampled, so no exemplar could be expected of it")
	}

	ctx = tenancy.WithTenant(ctx, tenancy.Tenant{ID: uuid.New(), Slug: "review16"})
	attrs := telemetry.MetricAttrs(ctx)
	in := telemetry.NewInstruments(provider.Meter(telemetry.Scope))
	in.ObserveOperation(ctx, 0.5, attrs...)
	in.ObserveOutboxLag(ctx, 3, attrs...)
	in.CountRefusal(ctx, "forbidden", attrs...)

	var got metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &got); err != nil {
		t.Fatalf("collect: %v", err)
	}
	want := span.SpanContext()
	seen := map[string]bool{}
	for _, scope := range got.ScopeMetrics {
		for _, m := range scope.Metrics {
			var exemplars []metricdata.Exemplar[float64]
			switch d := m.Data.(type) {
			case metricdata.Histogram[float64]:
				for _, p := range d.DataPoints {
					exemplars = append(exemplars, p.Exemplars...)
				}
			case metricdata.Gauge[float64]:
				for _, p := range d.DataPoints {
					exemplars = append(exemplars, p.Exemplars...)
				}
			case metricdata.Sum[int64]:
				for _, p := range d.DataPoints {
					for _, e := range p.Exemplars {
						exemplars = append(exemplars, metricdata.Exemplar[float64]{
							FilteredAttributes: e.FilteredAttributes, TraceID: e.TraceID, SpanID: e.SpanID,
						})
					}
				}
			}
			seen[m.Name] = true
			if len(exemplars) == 0 {
				t.Errorf("%s: no exemplar on any datapoint although %s/%s was recording when it was measured;"+
					" with the request id deliberately left off the datapoint, a number with no exemplar names no request",
					m.Name, want.TraceID(), want.SpanID())
				continue
			}
			for _, e := range exemplars {
				if hexID(e.TraceID) != want.TraceID().String() || hexID(e.SpanID) != want.SpanID().String() {
					t.Errorf("%s: exemplar names %s/%s, not the recording span %s/%s",
						m.Name, hexID(e.TraceID), hexID(e.SpanID), want.TraceID(), want.SpanID())
				}
			}
		}
	}
	for _, name := range []string{"pkit.http.operation.duration", "pkit.outbox.lag", "pkit.http.refusals"} {
		if !seen[name] {
			t.Errorf("no %s came back from the reader, so this case read nothing about it", name)
		}
	}
}

// hexID is the lower-case hex spelling a trace backend shows, from the raw bytes
// metricdata carries an exemplar id in.
func hexID(b []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, 0, len(b)*2)
	for _, c := range b {
		out = append(out, digits[c>>4], digits[c&0x0f])
	}
	return string(out)
}
