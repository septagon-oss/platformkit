package app

import (
	"context"
	"errors"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type unavailableTraceExporter struct{ sdktrace.SpanExporter }

func (unavailableTraceExporter) ExportSpans(context.Context, []sdktrace.ReadOnlySpan) error {
	return errors.New("trace export unavailable")
}

type availableMetricExporter struct{ sdkmetric.Exporter }

func (availableMetricExporter) Export(context.Context, *metricdata.ResourceMetrics) error {
	return nil
}

func TestAMetricSuccessDoesNotHideATraceExportFailure(t *testing.T) {
	record := &exported{}
	traces := traceExporter{SpanExporter: unavailableTraceExporter{}, rec: record}
	metrics := metricExporter{Exporter: availableMetricExporter{}, rec: record}
	if err := traces.ExportSpans(t.Context(), nil); err == nil {
		t.Fatal("the trace exporter did not reach its refusal")
	}
	if err := metrics.Export(t.Context(), &metricdata.ResourceMetrics{}); err != nil {
		t.Fatalf("the metric exporter should remain available: %v", err)
	}
	if message, err := record.Report(t.Context()); err == nil {
		t.Errorf("trace export is unavailable after a metric success, but health reported %q without an error", message)
	}
}
