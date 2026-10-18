package app

// Three places promise that a process which exports nothing
// still propagates the trace it was handed and still leaves one on what it publishes:
//
//	kit/telemetry/README.md  "Propagators(), which every PlatformKit process installs
//	                          whether or not it exports"
//	kit/config/config.go     a deployment that names no collector "still propagates a
//	                          trace it was handed and still leaves a trace context on
//	                          every outbox row it writes"
//	kit/events/trace.go      the outbox's traceparent/tracestate/baggage columns are
//	                          filled by otel.GetTextMapPropagator() — the global, and
//	                          kit/app is the only package in the kernel that installs it
//
// Nothing tested it: kit/telemetry/telemetry_test.go checks that telemetry.Propagators()
// exists, and every kernel test that needs a propagator installs one itself. The line
// that does the installing is the first statement of installTelemetry, above the
// `otlp_endpoint == ""` return. Slide it below that return — a tidy change that leaves
// exporting behaviour untouched — and every deployment that names no collector, which is
// the shipped default, starts writing NULL into those three columns and stops reading a
// client's traceparent. No case in this repository fails, /ready still answers ok, and the
// trace simply ends at the process boundary, which is the thing migrations/000028 exists
// to prevent. So the assertions are on the carrier the global writes and reads: the
// traceparent string and the baggage member, not on a provider or a type.
//
// The case installs its own starting propagator — an empty composite, which injects and
// extracts nothing — so what it measures is what installTelemetry left, not what another
// test in this binary happened to install, and restores it on the way out.

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/telemetry"
)

const (
	r9Hex   = "4bf92f3577b34da6a3ce929d0e0e4736"
	r9W3C   = "00-" + r9Hex + "-0102030405060708-01"
	r9ReqID = "req-0102030405060708"
)

func TestAProcessThatExportsNothingStillPropagatesTheTrace(t *testing.T) {
	previous := otel.GetTextMapPropagator()
	defer otel.SetTextMapPropagator(previous)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator())

	shutdown, report, err := installTelemetry(t.Context(),
		config.Telemetry{ServiceName: "pkit-no-export"}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("installTelemetry with no otlp_endpoint: %v", err)
	}
	if report != nil {
		t.Error("a process with no exporter reported one's health")
	}
	if err := shutdown(t.Context()); err != nil {
		t.Fatalf("shutdown of the off configuration: %v", err)
	}
	want, err := uuid.Parse(r9Hex)
	if err != nil {
		t.Fatalf("the fixture trace id: %v", err)
	}
	tid, sid := trace.TraceID(want), trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8}

	// Outbound: what kit/events writes into the outbox's three columns for a publisher
	// under this span, in a process that installs no provider.
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(
		trace.SpanContextConfig{TraceID: tid, SpanID: sid, TraceFlags: trace.FlagsSampled}))
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(telemetry.WithRequestID(ctx, r9ReqID), carrier)
	if got := carrier["traceparent"]; got != r9W3C {
		t.Errorf("the off configuration left traceparent %q on the carrier, want %q: with nothing "+
			"installed here, the outbox row's traceparent is NULL and the event's handler starts a "+
			"second, unrelated trace (migrations/000028)", got, r9W3C)
	}
	if got := carrier["baggage"]; !strings.Contains(got, telemetry.AttrRequestID+"="+r9ReqID) {
		t.Errorf("the off configuration left baggage %q on the carrier, want it to carry %s=%s",
			got, telemetry.AttrRequestID, r9ReqID)
	}

	// Inbound: the trace a client handed this process is the one a span below inherits.
	inherited := trace.SpanContextFromContext(otel.GetTextMapPropagator().Extract(
		context.Background(), propagation.MapCarrier{"traceparent": r9W3C}))
	if !inherited.IsValid() {
		t.Fatal("a client's traceparent was not inherited at all: the request's spans would " +
			"start a new trace instead of joining the caller's")
	}
	if inherited.TraceID() != tid {
		t.Errorf("the inherited trace is %s, want the client's %s", inherited.TraceID(), tid)
	}
}
