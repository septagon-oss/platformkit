package telemetry_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/telemetry"
)

// unreachable is a collector that is not there. The point of these tests is that
// tracing never gates the boot: an endpoint that is a valid URL is accepted
// whether or not anything answers it, because spans are a report about the
// application and not a dependency of it.
const unreachable = "http://127.0.0.1:1/v1/traces"

// sampled is whether the provider now installed records a span this process
// starts by itself. It is asked through the global API because that is the only
// way anything else in the process — otelhttp, a module's own instrumentation —
// asks it.
func sampled(t *testing.T, ctx context.Context) bool {
	t.Helper()
	ctx, span := otel.Tracer("telemetry.test").Start(ctx, "probe")
	defer span.End()
	return trace.SpanFromContext(ctx).IsRecording()
}

func remoteTrace(t *testing.T) context.Context {
	t.Helper()
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{1},
		SpanID:     trace.SpanID{2},
		TraceFlags: trace.FlagsSampled,
		Remote:     true,
	})
	return trace.ContextWithSpanContext(t.Context(), sc)
}

// TestTracingIsOffWithoutAnEndpoint is the default: no collector, no spans. The
// propagators are installed anyway, because a process that records nothing may
// still be handed a trace by a process that records something, and passing it on
// costs nothing and is the difference between one trace and two.
func TestTracingIsOffWithoutAnEndpoint(t *testing.T) {
	installed := otel.GetTracerProvider()
	t.Cleanup(func() { otel.SetTracerProvider(installed) })

	shutdown, err := telemetry.Start(t.Context(), config.Telemetry{}, nil)
	if err != nil {
		t.Fatalf("Start with no endpoint: %v", err)
	}
	if shutdown == nil {
		t.Fatal("Start returned no shutdown")
	}
	if err := shutdown(t.Context()); err != nil {
		t.Errorf("shutdown of the no-op provider: %v", err)
	}
	if _, span := otel.Tracer("telemetry.test").Start(t.Context(), "probe"); span.IsRecording() {
		t.Error("an endpoint-less boot installed a provider that records")
	}
	ctx := remoteTrace(t)
	carrier := propagation.MapCarrier{}
	propagators := otel.GetTextMapPropagator()
	propagators.Inject(ctx, carrier)
	if carrier["traceparent"] == "" {
		t.Error("the W3C propagator was not installed: an event published here carries no trace")
	}
	// Both members of the pair, and baggage beside them: a composite that left
	// one out would silently drop half of every trace that crossed a service.
	fields := " "
	for _, f := range propagators.Fields() {
		fields += f + " "
	}
	for _, want := range []string{"traceparent", "tracestate", "baggage"} {
		if !strings.Contains(fields, " "+want+" ") {
			t.Errorf("the installed propagators do not handle %s: they name %s", want, fields)
		}
	}
}

// TestAnInvalidEndpointIsRefusedBeforeAnythingIsInstalled: the provider that was
// there stays there. A half-installed tracer is the state in which a deployment
// serves traffic with a collector nobody told it about.
func TestAnInvalidEndpointIsRefusedBeforeAnythingIsInstalled(t *testing.T) {
	recording := sdktrace.NewTracerProvider()
	otel.SetTracerProvider(recording)
	t.Cleanup(func() { otel.SetTracerProvider(recording) })

	for _, bad := range []string{"collector:4318", "https://", "://nope", "https://collector:4318?token=a-secret", "https://user@collector:4318"} {
		shutdown, err := telemetry.Start(t.Context(), config.Telemetry{OTLPEndpoint: bad}, nil)
		if err == nil {
			t.Errorf("%q was accepted", bad)
			continue
		}
		if shutdown != nil {
			t.Errorf("%q returned a shutdown beside its error", bad)
		}
		if !sampled(t, t.Context()) {
			t.Fatalf("%q replaced the provider that was installed", bad)
		}
	}
	if err := recording.Shutdown(t.Context()); err != nil {
		t.Errorf("cleaning up: %v", err)
	}
}

// TestTheSampleRatioIsTheDecisionForATraceThisProcessStarts: what the key means,
// including the half a ratio cannot mean — a trace that arrived already sampled
// is kept, because that is the only way a request and the event it caused stay
// one trace. The service name is checked in the same place because a trace
// nobody can attribute is the same loss as one nobody can see.
func TestTheSampleRatioIsTheDecisionForATraceThisProcessStarts(t *testing.T) {
	nothing, everything := 0.0, 1.0
	for _, tc := range []struct {
		what    string
		ratio   *float64
		parent  bool
		sampled bool
	}{
		{"omitted means everything", nil, false, true},
		{"one", &everything, false, true},
		{"zero keeps no new trace", &nothing, false, false},
		{"zero does not break a trace somebody else started", &nothing, true, true},
	} {
		otel.SetTracerProvider(sdktrace.NewTracerProvider())
		cfg := config.Telemetry{OTLPEndpoint: unreachable, ServiceName: "acme-web", SampleRatio: tc.ratio}
		shutdown, err := telemetry.Start(t.Context(), cfg, nil)
		if err != nil {
			t.Fatalf("%s: %v", tc.what, err)
		}
		got := sampled(t, t.Context())
		if tc.parent {
			got = sampled(t, remoteTrace(t))
		}
		if got != tc.sampled {
			t.Errorf("%s: recording = %v, want %v", tc.what, got, tc.sampled)
		}
		if _, ok := otel.GetTracerProvider().(*sdktrace.TracerProvider); !ok {
			t.Fatal("an endpoint was configured and the provider is not the SDK's")
		}
		// A span that is not sampled carries no resource either, so this is read
		// where there is a span to read it from.
		if tc.sampled {
			probe := t.Context()
			if tc.parent {
				probe = remoteTrace(t)
			}
			_, span := otel.Tracer("telemetry.test").Start(probe, "probe")
			view, recorded := span.(sdktrace.ReadOnlySpan)
			span.End()
			if !recorded {
				t.Fatalf("the sampled span is a %T, not the SDK's", span)
			}
			name, found := view.Resource().Set().Value(attribute.Key("service.name"))
			if !found || name.AsString() != "acme-web" {
				t.Errorf("the resource says service.name %v", name)
			}
		}
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		if err := shutdown(ctx); err != nil {
			// Flushing to a collector that is not there is allowed to fail; what
			// matters is that it comes back rather than hanging the shutdown.
			t.Logf("%s: shutdown reported %v", tc.what, err)
		}
		cancel()
	}
}
