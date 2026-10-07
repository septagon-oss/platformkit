package app

// Review round 20 (T-0110). The one promise of this delivery that no committed case
// reaches through a real exporter is `kit/health`'s half of it. The brief asks that
// "kit/health reports the exporter's last success", and that report is produced by
// traceExporter/metricExporter wrapping the OTLP exporters and noting what they
// answered. The cases that exist feed `exported.note` an error the test itself made
// (resource_never_names_a_tenant_test.go, whose comment is explicit —
// "no case anywhere reads the report back") or assert the shape of the resource; and
// the review rounds' own "Unverified" lists name the gap in as many words: "What
// remains genuinely unexercised is a *real* OTLP export attempt against a dead
// endpoint, as opposed to the wrapped exporter being handed an error."
//
// That gap is where the interesting failure hides. If the SDK never hands a batch to
// the wrapped exporter on a process whose collector is unreachable — because the
// batch is dropped before Export, because the connection never becomes ready and the
// batcher gives up on its own, because the wrapper is attached to the reader and not
// to the provider — then `attempts` stays 0 and /ready answers "no export attempted
// yet" for the life of the process while the pipeline has been down since the boot.
// A hand-fed note() cannot see that, and neither can a reading of the source: it is a
// property of the wiring, and only an attempt against a dead address tests it.
//
// So this case dials a port that refuses, opens one sampled span, forces the flush,
// and reads the report back three times: before anything was tried (it must not
// imply a pipeline that works), after the failed attempt (it must say the last export
// failed, on the endpoint an anonymous client reaches), and never with the collector's
// address or the driver's sentence in it. Every assertion is reached through the
// report's own status and text — what the fixed behaviour prints — never through
// anything a broken one would print.
//
// It owns no figure and no tree-wide count: it reads what one boot of one process
// against one dead port answers, so no other branch can make it fail.
//
// One hazard, said out loud because it is the reason this case installs the globals
// rather than avoiding them: otel's global wrapper delegates to the provider installed
// *first* and to no later one (internal/global/state.go, delegateTraceOnce.Do), which is
// what kit/app/telemetry.go's header comment states as the rule. Another case in this
// package installs providers before this one — endpoint_form_test.go
// installs a real pair for "collector.example:4317" and for "localhost:4317" — so the
// wrapper's delegate is theirs by the time this case runs, and it stays theirs. That is
// the arrangement the composition claims for itself and the reason this case still sees
// its own span: telemetry.Tracer() takes the provider on every call, so once this case
// installs, otel.GetTracerProvider() answers this provider, and the tracer it hands out
// records here. Restoring what this case captured puts the wrapper back as GetTracerProvider's
// answer, which is the state it found. `go test -count=1 ./kit/app/`, whole package, is
// green with this case in it (58.9s), which is what says the arrangement is stable.

import (
	"context"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/telemetry"
)

// deadCollector is a port the kernel just handed back and this test closed: what dials
// it is refused at once, which is a dead collector without a container.
func deadCollector(t *testing.T) string {
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

func TestACollectorThatNeverAnsweredIsReportedAsDown(t *testing.T) {
	where := deadCollector(t)

	prevTraces, prevMetrics := otel.GetTracerProvider(), otel.GetMeterProvider()
	prevHandler := otel.GetErrorHandler()
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(error) {}))
	shutdown, report, err := installTelemetry(context.Background(), config.Telemetry{
		ServiceName:  "pkit-review20",
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
		_ = shutdown(ctx)
		otel.SetTracerProvider(prevTraces)
		otel.SetMeterProvider(prevMetrics)
		otel.SetErrorHandler(prevHandler)
	})

	// Before anything was tried, the reading may not imply a working pipeline.
	firstMsg, firstErr := report.Report(context.Background())
	if firstErr != nil || !strings.Contains(firstMsg, "no export attempted") {
		t.Errorf("before the first attempt /ready reads %q (%v): a process with no reading of the "+
			"collector says neither that it works nor that nothing has been asked", firstMsg, firstErr)
	}

	// One span, sampled, ended, so the batcher holds something it has to send.
	_, span := telemetry.Tracer().Start(context.Background(), "review20.probe")
	span.End()

	// No deadline of this test's on the flush: the exporter bounds its own attempt
	// (ten seconds), and a shorter one is this case racing the SDK and losing — which
	// is how a first draft of this file failed for the wrong reason.
	flusher, ok := otel.GetTracerProvider().(interface {
		ForceFlush(context.Context) error
	})
	if !ok {
		t.Fatal("the installed TracerProvider cannot be flushed, so nothing can ask it what the collector said")
	}
	_ = flusher.ForceFlush(context.Background()) // expected to fail; the report is what is read

	msg, rerr := report.Report(context.Background())
	if rerr == nil {
		t.Errorf("after an export attempt against a port that refuses it, /ready answers %q with no error: "+
			"a pipeline that has taken nothing since the boot reads as alive to anybody who can poll it "+
			"(pillar contract 5; a provider failure is an unavailable reading, never a silent allow)", msg)
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
