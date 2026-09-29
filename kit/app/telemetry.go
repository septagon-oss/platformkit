package app

// This file is where the process's two providers are chosen and installed, which
// is the brief's own home for them: one TracerProvider and one MeterProvider, in
// kit/app. Nothing else in the kernel may install one — kit/httpx, kit/db,
// kit/events and kit/jobs make spans and record numbers through the OpenTelemetry
// API and kit/telemetry's vocabulary, and never import this file's packages.
//
// The install happens once per process, in New, and that is a fact about
// OpenTelemetry rather than a preference: each global delegates to the provider
// installed *first*, and a later install changes only what a provider lookup
// answers and the tracers made after it. The kernel's packages take their tracers
// and their meter at initialization, so a second install here would leave them
// reporting to the first forever.
//
// Tracing and metrics are off when telemetry.otlp_endpoint is empty, and "off"
// here means *nothing is installed*: the global's default provider is already the
// no-op one, so installing a no-op would buy nothing and would spend the one
// install a process gets — which is what lets a test, or an embedding application,
// install a recorder of its own instead.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	exportermetric "go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	exportertrace "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdkresource "go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/health"
	"github.com/septagon-oss/platformkit/kit/telemetry"
)

// exported is the record of what the collector last took. kit/health reports it:
// an operator asking "is my trace pipeline alive" is asking about the exporter's
// last success, and nothing else in the process knows the answer.
//
// It is a mutex and three fields rather than an atomic because the three move
// together, and a report that pairs a success timestamp with a different
// attempt's failure would be a worse answer than a stale one.
type exported struct {
	mu       sync.Mutex
	last     time.Time
	attempts int
	err      error
}

func (e *exported) note(err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.attempts++
	if err == nil {
		e.last = db.Now()
	}
	e.err = err
}

// traceExporter wraps the OTLP span exporter to record what it answered. The
// error is returned unchanged: whether a batch arrived is the collector's
// business and the SDK's retry policy, and this only writes down the answer.
type traceExporter struct {
	sdktrace.SpanExporter
	rec *exported
}

func (t traceExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	err := t.SpanExporter.ExportSpans(ctx, spans)
	t.rec.note(err)
	return err
}

// metricExporter wraps the OTLP metric exporter for the same reason.
type metricExporter struct {
	sdkmetric.Exporter
	rec *exported
}

func (m metricExporter) Export(ctx context.Context, data *metricdata.ResourceMetrics) error {
	err := m.Exporter.Export(ctx, data)
	m.rec.note(err)
	return err
}

// errExportDown is the report's half of the answer, and it is a sentence of this
// composition's rather than the exporter's error. The raw error names what it could
// not reach — "dial otel-collector.observability.svc.cluster.local:4317: connect:
// connection refused" — and /ready answers that on the listener a client reaches,
// anonymously and for as long as the pipeline is down: a service name, a port and a
// namespace, in the shape this repository's own deployment recommends. Whether the
// pipeline is down and for how long is what an operator reading /ready asks, and
// OpenTelemetry's own error handler carries the cause to the process log, where the
// address belongs.
var errExportDown = errors.New("the collector is not taking this process's data")

// Report is this composition's answer to "did the collector take it". It is a
// health.Report and not a health.Check, and the difference is the whole point: a
// readiness check that fails when the trace backend is down takes traffic off a
// replica that is serving its tenants perfectly, which makes tracing the reason an
// application stopped answering — the one failure this file exists to refuse. So
// the exporter's last success is reported beside the verdict and never decides it.
func (e *exported) Report(context.Context) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.attempts == 0 {
		return "no export attempted yet", e.err
	}
	if e.err != nil {
		return fmt.Sprintf("%d exports attempted, last one failed", e.attempts), errExportDown
	}
	return fmt.Sprintf("exported %s ago", db.Now().Sub(e.last).Truncate(time.Second)), nil
}

// Name is the report's own, so /ready names the pipeline it is talking about.
func (*exported) Name() string { return "telemetry" }

var _ health.Report = (*exported)(nil)

// metricExporter is a metric.Exporter by construction, and saying so here turns a
// change to that contract into a compile error in this file rather than a report
// that quietly stopped being written.

// installTelemetry chooses the providers and installs them. It returns the
// shutdown that flushes both, and the health.Report that names the exporter's
// last success — nil when nothing is exported, because an exporter that does not
// exist has no success to report and "off" is what an operator should read.
//
// An endpoint that is not a URL is refused: a typo in a key that decides where
// spans go should stop the boot, not leave a process half-traced. A bare host:port is
// not that refusal — it is the http:// URL it names, with the scheme left off, and it
// is the form config.example.yaml prints; see collector. A collector that is
// unreachable is not either: the exporters buffer, warn through OpenTelemetry's own
// error handler, and the application starts — and the last flush of a process that
// is leaving says the same thing in the log rather than as an exit code.
func installTelemetry(ctx context.Context, cfg config.Telemetry, log *slog.Logger) (func(context.Context) error, health.Report, error) {
	otel.SetTextMapPropagator(telemetry.Propagators())
	if cfg.OTLPEndpoint == "" {
		log.DebugContext(ctx, "telemetry: measurement is off; telemetry.otlp_endpoint is empty")
		return func(context.Context) error { return nil }, nil, nil
	}
	where, err := collector(cfg.OTLPEndpoint)
	if err != nil {
		return nil, nil, err
	}
	rec := &exported{}
	traces, err := exportertrace.New(ctx, exportertrace.WithEndpointURL(where))
	if err != nil {
		return nil, nil, fmt.Errorf("app: telemetry: %w", err)
	}
	metrics, err := exportermetric.New(ctx, exportermetric.WithEndpointURL(where))
	if err != nil {
		return nil, nil, fmt.Errorf("app: telemetry: %w", err)
	}
	res, err := resource(cfg)
	if err != nil {
		return nil, nil, err
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExporter{SpanExporter: traces, rec: rec}),
		// ParentBased, so a request that arrived sampled stays one trace through
		// the outbox and the worker: the ratio is the decision for a trace this
		// process starts and never a veto on one it was handed.
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.Ratio()))),
		sdktrace.WithResource(res),
	)
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter{Exporter: metrics, rec: rec})),
		sdkmetric.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(mp)
	log.InfoContext(ctx, "telemetry: exporting", "endpoint", where, "service", cfg.ServiceName,
		"client", cfg.Client, "sample_ratio", cfg.Ratio())
	// One shutdown for both, and both are flushed even if one of them cannot be
	// delivered. An export that failed at the end of a process's life is not a
	// shutdown that failed: it is the same fact the health.Report above carries —
	// the collector did not take the data — and the reasoning is the one this file
	// already gives for /ready. Returning it would turn "the trace backend is down"
	// into a non-zero exit, a failed systemd unit or a Job that never completes, for
	// an application that served its tenants the whole time; what the cause was goes
	// to the log, with the address, where an operator debugging the pipeline reads
	// it. Every other error Join can answer is an export error too — a provider that
	// has already shut down says so by returning nil — so nothing else is hidden.
	shutdown := func(ctx context.Context) error {
		if err := errors.Join(tp.Shutdown(ctx), mp.Shutdown(ctx)); err != nil {
			log.ErrorContext(ctx, "telemetry: the last batch of spans or metrics did not reach the collector",
				"endpoint", where, "service", cfg.ServiceName, "error", err)
		}
		return nil
	}
	return shutdown, rec, nil
}

// resource is the trace's and the metric's origin: service.name always, and
// pkit.client when the deployment named one. The SDK's own attributes and whatever
// OTEL_RESOURCE_ATTRIBUTES says stay; this overrides only the name.
//
// No tenant, and no client either when the installation is shared. A resource
// describes the process, and this process serves many tenants — so the tenant
// arrives on the spans, where a request put it, and not here. kit/telemetry's
// package comment carries the argument; this is where it is enforced.
func resource(cfg config.Telemetry) (*sdkresource.Resource, error) {
	attrs := []attribute.KeyValue{attribute.String("service.name", cfg.ServiceName)}
	if cfg.Client != "" {
		attrs = append(attrs, attribute.String(telemetry.AttrClient, cfg.Client))
	}
	// Schemaless, so merging with the SDK's default resource keeps that resource's
	// schema URL instead of refusing two schema URLs in one merge. service.name
	// predates every version of the semconv package that would otherwise supply it.
	merged, err := sdkresource.Merge(sdkresource.Default(), sdkresource.NewSchemaless(attrs...))
	if err != nil {
		return nil, fmt.Errorf("app: telemetry: %w", err)
	}
	return merged, nil
}

// collector is the endpoint as the exporters are given it — and the check that
// refuses a value they could not use, so a typo in a key that decides where spans
// go stops the boot instead of leaving a process half-traced.
//
// A bare host:port is normalised to its http:// URL rather than refused, because it
// is the same collector with the scheme left off, and it is the form both
// config.example.yaml and config.Telemetry's own comment print — the ordinary answer
// for a collector on a network that needs no TLS. Refusing it would make the file an
// operator copies describe a configuration that does not start. http is what an
// address without a scheme means: WithEndpointURL reads any scheme but https as "no
// transport security", which is the sentence the two documents use for host:port.
//
// The normalised value is what the exporters are handed and what the log line names,
// so what was asked for, what was understood and what is dialed are one string. A
// value no URL can be — a scheme that is not this exporter's, credentials, a query,
// an address inside the scheme where the host should be — is still refused.
func collector(raw string) (string, error) {
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		(u.Scheme != "http" && u.Scheme != "https") {
		return "", errors.New("app: telemetry.otlp_endpoint must be an http:// or https:// " +
			"collector URL, or the host:port of one that needs no TLS")
	}
	return u.String(), nil
}
