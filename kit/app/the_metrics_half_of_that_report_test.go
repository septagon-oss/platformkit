package app

// Review round 21 (T-0110). A pin, not a defect: it closes the one line of round 20's
// own "Unverified" list that a case could close.
//
//	"The metric half of the same promise. My case proves a failed *span* export
//	 reaches the report. The periodic metric reader ticks every fifteen seconds and
//	 its failed export goes through the sibling `metricExporter` wrapper; I never
//	 waited a tick, so that half is unexercised."  — REVIEW round 20, Unverified
//
// The brief's line is "kit/health reports the exporter's last success", and this
// composition wraps *two* exporters (kit/app/telemetry.go: traceExporter,
// metricExporter) into one record. Round 20 reached the trace half through a real
// OTLP attempt. The metric half was only read in the source: a wrapper attached to
// a reader that never ticks, a reader that never exports at shutdown, a
// `note` call that only the trace path reaches, and the process would keep a
// collector that took no numbers at all out of the operator's reading — while
// /ready, which any client may poll, says nothing about it.
//
// So this case asks the question the metrics half answers, with the trace half
// deliberately silent. No span is ended, on purpose:
// batchSpanProcessor.exportSpans (go.opentelemetry.io/otel/sdk@v1.46.0
// trace/batch_span_processor.go:299) calls the wrapped exporter only `if l :=
// len(bsp.batch); l > 0`, and bsp.Shutdown (:162) calls the exporter's Shutdown and
// not ExportSpans, so a process that ended no spans records no trace attempt at
// all. What the report can therefore be carrying after the composition's own
// shutdown is the metric reader's: PeriodicReader.Shutdown
// (go.opentelemetry.io/otel/sdk/metric@v1.46.0 periodic_reader.go:390-412) collects
// once and calls `r.exporter.Export` whatever the collection held. If that call
// never reached this file's wrapper, `attempts` would stay 0 and the reading would
// be the one from before the boot — which is the branch that makes this a case with
// something to lose, and not a description of the source.
//
// It reads the report through what the fixed behaviour prints: the report's own
// status (an error or none), its own words, and the absence of the collector's
// address. Nothing here waits on a wall-clock tick of fifteen seconds — the flush
// is the one a leaving process performs — and nothing here depends on which
// provider the otel globals delegated to first: the metric exporter, its wrapper
// and the record they write belong to the providers installTelemetry built, and
// the shutdown it returned holds them by closure.
//
// It owns no figure and no whole-tree count, so no other branch landing beside this
// one can make it fail.

import (
	"context"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"

	"github.com/septagon-oss/platformkit/kit/config"
)

// deadPort21 is a port the kernel handed back and this case closed at once: what
// dials it is refused immediately, which is a dead collector without a container.
// Named apart from round 20's helper rather than shared with it, so that file
// remains the reviewer's bytes and this one stands on its own.
func deadPort21(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("binding a port to hand back dead: %v", err)
	}
	where := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("closing it: %v", err)
	}
	return where
}

func TestALeavingProcessReportsWhatTheCollectorSaidAboutItsNumbers(t *testing.T) {
	where := deadPort21(t)

	prevTraces, prevMetrics := otel.GetTracerProvider(), otel.GetMeterProvider()
	prevHandler := otel.GetErrorHandler()
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(error) {}))
	shutdown, report, err := installTelemetry(context.Background(), config.Telemetry{
		ServiceName:  "pkit-review21",
		OTLPEndpoint: where,
	}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("a collector that refuses connections must not stop the boot: %v", err)
	}
	if report == nil {
		t.Fatal("a process that exports registers no health.Report at all")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdown(ctx) // already shut down; this only releases the globals below
		otel.SetTracerProvider(prevTraces)
		otel.SetMeterProvider(prevMetrics)
		otel.SetErrorHandler(prevHandler)
	})

	// Nothing has been asked of the collector yet. The reading may not imply a
	// working pipeline, and it may not invent a failure either.
	firstMsg, firstErr := report.Report(context.Background())
	if firstErr != nil || !strings.Contains(firstMsg, "no export attempted") {
		t.Errorf("before the first attempt /ready reads %q (%v): a process with no reading of the "+
			"collector says neither that it works nor that nothing has been asked", firstMsg, firstErr)
	}

	// The flush a process performs on its way out — the composition's own shutdown,
	// not this case reaching into a provider. No span ended above, so the only
	// export it can record is the metric reader's; see the header for the SDK lines
	// that make that true. The bound is the caller's but a loose one: the reader
	// applies its own ten-second export timeout, and a case that raced it would
	// fail for its own impatience.
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	if err := shutdown(ctx); err != nil {
		t.Errorf("a process whose collector took nothing exits as a failing one (%v): an unreachable "+
			"trace backend is not a service that stopped serving its tenants, and this file says so at "+
			"the shutdown it returns", err)
	}

	msg, rerr := report.Report(context.Background())
	if rerr == nil {
		t.Errorf("after the export a leaving process makes against a port that refuses it, /ready answers "+
			"%q with no error: the metric half of the pipeline is unreported, so a collector that has "+
			"taken no number since the boot reads as alive to anybody who can poll it "+
			"(pillar contract 5 — a provider failure is an unavailable reading, never a silent allow)", msg)
	}
	if !strings.Contains(msg, "failed") {
		t.Errorf("the report is %q, which does not say the last export failed", msg)
	}
	if strings.Contains(msg, "exported") {
		t.Errorf("the report is %q, which claims a delivery the collector refused", msg)
	}
	text := msg
	if rerr != nil {
		text = msg + " " + rerr.Error()
	}
	for _, leak := range []string{"127.0.0.1", ":", "refused", "dial", "connect", "Unavailable"} {
		if strings.Contains(text, leak) {
			t.Errorf("the reading an anonymous client gets names %q of the collector's address or the "+
				"driver's own sentence: %q (%v)", leak, msg, rerr)
		}
	}
}
