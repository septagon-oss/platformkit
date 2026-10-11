package telemetry_test

// Tracer is the one door this package opens onto the OpenTelemetry global, and it
// has one property worth a case: which provider a span arrives at is decided when
// the span is opened, and not when this package was initialised.
//
// The property has no case of its own anywhere, and the shape that found its
// absence is the common one — a kernel package that held `var tracer =
// otel.Tracer(Scope)`, and a test that installs a recorder after the package was
// loaded and reads back nothing at all. That is a silent failure: the spans the
// caller opened were recorded, somewhere it was not looking, while everything the
// caller did looks like it worked. The case below is that failure drawn to one
// size, with no database, no relay and no delivery to hide behind.

import (
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/septagon-oss/platformkit/kit/telemetry"
)

// TestASpanArrivesAtTheProviderInstalledWhenItOpened asserts both halves of the
// one-call-one-provider rule: the provider installed *before* a span is opened gets
// that span, and the one installed afterwards gets the next and not the first.
func TestASpanArrivesAtTheProviderInstalledWhenItOpened(t *testing.T) {
	first, second := tracetest.NewSpanRecorder(), tracetest.NewSpanRecorder()

	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(first)))
	_, span := telemetry.Tracer().Start(t.Context(), "under the first provider")
	span.End()

	// The reachability half, off the span this call just made: a recorder nothing
	// ever reached reads exactly like a kernel that opened no span, and the case
	// would pass by measuring nothing.
	if got := names(first.Ended()); len(got) != 1 || got[0] != "under the first provider" {
		t.Fatalf("the provider installed before the span holds %v, want the one span opened under it", got)
	}

	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(second)))
	_, span = telemetry.Tracer().Start(t.Context(), "under the second provider")
	span.End()

	if got := names(second.Ended()); len(got) != 1 || got[0] != "under the second provider" {
		t.Errorf("the provider installed second holds %v, want only the span opened under it: a tracer "+
			"bound at package initialisation keeps writing to the first provider the process installed, "+
			"which is the failure this case exists to notice", got)
	}
	if got := names(first.Ended()); len(got) != 1 {
		t.Errorf("the first provider holds %v after the second was installed, want the one span it was "+
			"opened under — a span that reaches an earlier provider is one its own reader never sees", got)
	}
}

// names is the recorder's content as span names, so a failure says what arrived.
func names(spans []sdktrace.ReadOnlySpan) []string {
	out := make([]string, 0, len(spans))
	for _, s := range spans {
		out = append(out, s.Name())
	}
	return out
}
