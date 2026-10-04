package app

// The reviewer's mixed_export_outcome_test.go asks whether a metric success hides a
// trace failure. These are the three readings the same record owes either way, and
// each one fails on its own:
//
//   - the mirror, where the spans arrive and the numbers do not, which is the
//     branch a shared record got right by accident and which says the cure is a
//     per-signal record rather than "the trace half wins";
//   - the reading an operator gets, which has to name which of the two exports is
//     down: "/ready" that says only "the last export failed" sends somebody to
//     debug the collector half that is answering;
//   - recovery, which no case held before: both wrappers succeed again after a
//     refusal, so the alarm has to clear, or a collector that blinked leaves a
//     process reading as down for the rest of its life.
//
// Nothing here waits on the reader's fifteen-second tick or on a batch filling: the
// attempts are the ones the wrappers make when the SDK calls them, which is the
// same path the two live-collector cases (review rounds 20 and 21) reach through a
// port that refuses connections.

import (
	"context"
	"errors"
	"strings"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// failingTraceExporter and failingMetricExporter answer one way until told
// otherwise, which is how the recovery case flips a half without rebuilding the
// providers or the record.
type failingTraceExporter struct {
	sdktrace.SpanExporter
	err error
}

func (f *failingTraceExporter) ExportSpans(context.Context, []sdktrace.ReadOnlySpan) error {
	return f.err
}

type failingMetricExporter struct {
	sdkmetric.Exporter
	err error
}

func (f *failingMetricExporter) Export(context.Context, *metricdata.ResourceMetrics) error {
	return f.err
}

func TestEachExportAnswersForItself(t *testing.T) {
	down := errors.New("the collector refused this batch")
	traces := &failingTraceExporter{err: down}
	metrics := &failingMetricExporter{err: down}
	rec := &exported{}
	spans := traceExporter{SpanExporter: traces, rec: rec}
	numbers := metricExporter{Exporter: metrics, rec: rec}
	export := func(t *testing.T) {
		t.Helper()
		if err := spans.ExportSpans(t.Context(), nil); !errors.Is(err, traces.err) {
			t.Fatalf("the span wrapper answered %v where its exporter answered %v", err, traces.err)
		}
		if err := numbers.Export(t.Context(), &metricdata.ResourceMetrics{}); !errors.Is(err, metrics.err) {
			t.Fatalf("the metric wrapper answered %v where its exporter answered %v", err, metrics.err)
		}
	}

	// Both halves refused. The reading says so, and says which.
	export(t)
	both, err := rec.Report(t.Context())
	if err == nil || !strings.Contains(both, "trace") || !strings.Contains(both, "metric") {
		t.Errorf("with both exports refused /ready reads %q (%v): an operator has to be able to tell "+
			"which pipeline to debug, and a refusal is not a reading without an error beside it", both, err)
	}

	// The spans came back and the numbers did not: the mirror of the mixed case.
	traces.err = nil
	export(t)
	only, err := rec.Report(t.Context())
	if err == nil || !strings.Contains(only, "metric") || strings.Contains(only, "the last trace export failed") {
		t.Errorf("with only the metric export refused /ready reads %q (%v): a delivery that arrived is "+
			"no reason to hide the one that did not, and no reason to name it as failing either", only, err)
	}

	// The numbers came back too. Whatever the operator was told while the
	// collector was down is not what they are told once it is not.
	metrics.err = nil
	export(t)
	recovered, err := rec.Report(t.Context())
	if err != nil || !strings.Contains(recovered, "exported") || strings.Contains(recovered, "failed") {
		t.Errorf("after both exports succeeded again /ready reads %q (%v): a collector that blinked "+
			"leaves this process reading as down for the rest of its life", recovered, err)
	}
}

// TestAHalfThatWasNeverAskedIsNotReportedAsDown covers the silent half of the same
// reading. A process that has exported numbers and ended no span has one unattempted
// export and one delivered one; the report that invents a failure for the half
// nobody asked would fail the case round 20 wrote the "no export attempted" reading
// for, from the other side.
func TestAHalfThatWasNeverAskedIsNotReportedAsDown(t *testing.T) {
	rec := &exported{}
	numbers := metricExporter{Exporter: &failingMetricExporter{}, rec: rec}
	if err := numbers.Export(t.Context(), &metricdata.ResourceMetrics{}); err != nil {
		t.Fatalf("the metric exporter should remain available: %v", err)
	}
	msg, err := rec.Report(t.Context())
	if err != nil || !strings.Contains(msg, "exported") {
		t.Errorf("/ready reads %q (%v) for a process whose only export so far arrived: an export nobody "+
			"attempted is not an export that failed", msg, err)
	}
}
