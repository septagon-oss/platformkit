package app

// This file is where the process's two providers are chosen and installed, which
// is the brief's own home for them: one TracerProvider and one MeterProvider, in
// kit/app. Nothing else in the kernel may install one — kit/httpx, kit/db,
// kit/events and kit/jobs make spans and record numbers through the OpenTelemetry
// API and kit/telemetry's vocabulary, and never import this file's packages.
//
// The install happens once per *started* application, at the end of Start, and the
// half of it that survives is a fact about OpenTelemetry rather than a preference:
// each global delegates to the provider installed *first*, and a later install
// changes only what a provider lookup answers and the tracers made after it. The
// kernel's tracers are taken as each span opens (telemetry.Tracer), so a span
// answers to the provider that was installed when it opened; its meter was taken
// once, when kit/telemetry was first used, so the instruments of the process answer
// to the first provider installed here and to no later one. Either way a second
// install in this package would buy a process two answers to one question and use
// the first — which is why a composition that is refused gets no vote. Installing is
// the one change a boot makes in its process that no release gives back: the pool,
// the store, the transport and the event shapes a composition declares all come back
// on a refused boot or a Close, and otel.SetTracerProvider has no undo worth doing,
// because a "previous" provider is only the last one somebody else installed. So it
// belongs where the boot stops being refusable, and a plan carries the decision from
// the gate that reads the configuration to the call that installs it (telemetryPlan).
//
// Tracing and metrics are off when telemetry.otlp_endpoint is empty, and "off"
// here means *no provider is installed*: the global's default provider is already the
// no-op one, so installing a no-op would buy nothing and would spend the one
// install a process gets — which is what lets a test, or an embedding application,
// install a recorder of its own instead. The propagator is still installed, because
// a process that exports nothing still carries the trace it was handed.

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

// signal is one of the process's two exports and what the collector last said
// about it. They are kept apart because they are two calls, to two services, on
// two schedules: spans leave in batches as they fill and numbers leave on the
// reader's tick, so one may arrive while the other is refused. The record that
// did not keep them apart is review 27's HIGH: one `err` for both halves meant a
// metric tick that arrived overwrote the failed span batch, and an operator
// asking "/ready" about the trace pipeline was answered with the metric
// pipeline's delivery.
//
// A signal is named by the field of `exported` it sits in, not by a field of its
// own, so the zero value of the record — the one a test builds and the one
// installTelemetry builds — is a record of two halves that have never been asked.
type signal struct {
	attempts int
	last     time.Time
	err      error
}

// note records one attempt of this signal. A success clears only this signal's
// own failure, which is the whole reason the failure lives here and not above.
func (s *signal) note(err error) {
	s.attempts++
	if err == nil {
		s.last = db.Now()
	}
	s.err = err
}

// exported is the record of what the collector last took. kit/health reports it:
// an operator asking "is my trace pipeline alive" is asking about the exporter's
// last success, and nothing else in the process knows the answer.
//
// It is a mutex and two signals rather than an atomic because the pair is read
// as one verdict, and a report that paired one half's failure with the other
// half's timestamp would be a worse answer than a stale one.
type exported struct {
	mu      sync.Mutex
	traces  signal
	metrics signal
}

// noteTraces and noteMetrics record one attempt of one export. The wrapper names
// which signal it is asking about; the record cannot tell the two exporters
// apart, and answering for a signal it was not told about is the defect above.
func (e *exported) noteTraces(err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.traces.note(err)
}

func (e *exported) noteMetrics(err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.metrics.note(err)
}

// note records an attempt nobody attributed to either half. The two wrappers
// above always name theirs, so the kernel writes no attempt here; what lives
// here is the record read as one pipeline, which is how two reviewers pinned it
// (review_round3_resource_never_names_a_tenant_test.go and
// review_round10_an_export_failure_names_no_address_test.go both build an
// `exported`, hand it the collector's refusal and read /ready's answer). An
// attempt nobody can place is held against both halves, because the honest
// reading of a delivery nobody can attribute is that neither half can claim it.
func (e *exported) note(err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.traces.note(err)
	e.metrics.note(err)
}

// traceExporter wraps the OTLP span exporter to record what it answered. The
// error is returned unchanged: whether a batch arrived is the collector's
// business and the SDK's retry policy, and this only writes down the answer — of
// the trace half, which is the only thing this wrapper knows.
type traceExporter struct {
	sdktrace.SpanExporter
	rec *exported
}

func (t traceExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	err := t.SpanExporter.ExportSpans(ctx, spans)
	t.rec.noteTraces(err)
	return err
}

// metricExporter wraps the OTLP metric exporter for the same reason, and for the
// metric half alone: its tick arriving says nothing about a span batch.
type metricExporter struct {
	sdkmetric.Exporter
	rec *exported
}

func (m metricExporter) Export(ctx context.Context, data *metricdata.ResourceMetrics) error {
	err := m.Exporter.Export(ctx, data)
	m.rec.noteMetrics(err)
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
//
// The verdict is the worst of the two halves, and the message names the half that
// failed: a reading that said only "the last export failed" would leave an
// operator debugging the wrong pipeline, and one that named the half which is
// still arriving would read as a delivery the other half refused. "no export
// attempted yet" is the one reading with no failure to attribute, and an
// unattempted half is silent rather than assumed down.
func (e *exported) Report(context.Context) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	total := e.traces.attempts + e.metrics.attempts
	if total == 0 {
		return "no export attempted yet", nil
	}
	var down []string
	for _, half := range []struct {
		name string
		s    *signal
	}{
		{"trace", &e.traces},
		{"metric", &e.metrics},
	} {
		if half.s.attempts > 0 && half.s.err != nil {
			down = append(down, "the last "+half.name+" export failed")
		}
	}
	if len(down) > 0 {
		return fmt.Sprintf("%d exports attempted, %s", total, strings.Join(down, " and ")), errExportDown
	}
	last := e.traces.last
	if e.metrics.last.After(last) {
		last = e.metrics.last
	}
	return fmt.Sprintf("exported %s ago", db.Now().Sub(last).Truncate(time.Second)), nil
}

// Name is the report's own, so /ready names the pipeline it is talking about.
func (*exported) Name() string { return "telemetry" }

var _ health.Report = (*exported)(nil)

// Both wrappers are the exporter contracts they wrap, by construction, and saying
// so here turns a change to either SDK contract into a compile error in this file
// rather than a report that quietly stopped being written.
var (
	_ sdktrace.SpanExporter = traceExporter{}
	_ sdkmetric.Exporter    = metricExporter{}
)

// telemetryPlan is one deployment's measurement, decided and refused with nothing
// yet changed in the process. New makes it and Start installs it.
//
// The split is the answer to what installing costs: a provider put in front of the
// process stays there, and the best an undo could do is reinstate whichever provider
// was standing last before it, which is a fact about other people's boots rather than
// a release. So the address, the exporters and the resource are settled at the plan,
// where every collector problem this file can find is still answered free of the pool,
// the migration and the port, and the two SDK providers are made and installed by
// Start, whose last act it is. A composition refused above that is refused with the
// process's providers exactly where they were, which is what README's "a refused
// build changes nothing in the process" means for the trace pipeline.
type telemetryPlan struct {
	cfg config.Telemetry
	log *slog.Logger

	// where is the collector's address as collector normalises it, and "" exactly
	// when telemetry.otlp_endpoint is empty. That is what "off" means here: an
	// address to install nothing behind.
	where string

	// traces, metrics and res are made at the plan, not at the install, so that a
	// header or a TLS pair no exporter can use stops the boot beside a bad endpoint,
	// above everything a deployment pays for. Neither exporter has reached the
	// network by then: the OTLP clients dial on their first export.
	traces  sdktrace.SpanExporter
	metrics sdkmetric.Exporter
	res     *sdkresource.Resource

	// rec is the record the two wrappers write and the report kit/health reads. It
	// exists from the plan, because the question /ready asks has an honest answer —
	// "no export attempted yet" — before anything is installed.
	rec *exported

	// shutdown is the flush install hands back, and nil until it runs: what a
	// process still owes a collector is a debt of the providers it installed.
	shutdown func(context.Context) error
}

// planTelemetry answers every refusal this file can answer and installs nothing.
// It returns the plan, the health.Report that names the exporter's last success —
// nil when nothing will be exported, because an exporter that does not exist has no
// success to report and "off" is what an operator should read — and the refusal.
//
// An endpoint that is not a URL is refused: a typo in a key that decides where
// spans go should stop the boot, not leave a process half-traced. A bare host:port is
// not that refusal — it is the http:// URL it names, with the scheme left off, and it
// is the form config.example.yaml prints; see collector. A collector that is
// unreachable is not either: the exporters buffer, warn through OpenTelemetry's own
// error handler, and the application starts — and the last flush of a process that
// is leaving says the same thing in the log rather than as an exit code.
func planTelemetry(ctx context.Context, cfg config.Telemetry, log *slog.Logger) (*telemetryPlan, health.Report, error) {
	plan := &telemetryPlan{cfg: cfg, log: log, rec: &exported{}}
	if cfg.OTLPEndpoint == "" {
		log.DebugContext(ctx, "telemetry: measurement is off; telemetry.otlp_endpoint is empty")
		plan.shutdown = func(context.Context) error { return nil }
		return plan, nil, nil
	}
	where, err := collector(cfg.OTLPEndpoint)
	if err != nil {
		return nil, nil, err
	}
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
	plan.where, plan.traces, plan.metrics, plan.res = where, traces, metrics, res
	return plan, plan.rec, nil
}

// install makes the two providers and puts them, and this deployment's propagators,
// in front of the process. Start calls it as the last thing a boot does, with nothing
// left to refuse, and it answers nothing: an install that could still fail would have
// to be given back, which is the cost this position exists to avoid.
func (p *telemetryPlan) install(ctx context.Context) {
	otel.SetTextMapPropagator(telemetry.Propagators())
	if p.where == "" {
		// Measurement off: the global's no-op provider is already what an empty
		// address wants, and spending the process's one install on it would only
		// stand between this process and a recorder its host wants to install.
		return
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExporter{SpanExporter: p.traces, rec: p.rec}),
		// ParentBased, so a request that arrived sampled stays one trace through
		// the outbox and the worker: the ratio is the decision for a trace this
		// process starts and never a veto on one it was handed.
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(p.cfg.Ratio()))),
		sdktrace.WithResource(p.res),
	)
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter{Exporter: p.metrics, rec: p.rec})),
		sdkmetric.WithResource(p.res),
	)
	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(mp)
	p.log.InfoContext(ctx, "telemetry: exporting", "endpoint", p.where, "service", p.cfg.ServiceName,
		"client", p.cfg.Client, "sample_ratio", p.cfg.Ratio())
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
	p.shutdown = func(ctx context.Context) error {
		if err := errors.Join(tp.Shutdown(ctx), mp.Shutdown(ctx)); err != nil {
			p.log.ErrorContext(ctx, "telemetry: the last batch of spans or metrics did not reach the collector",
				"endpoint", p.where, "service", p.cfg.ServiceName, "error", err)
		}
		return nil
	}
}

// flush pushes what this composition's own providers still hold. It is nothing at
// all for a plan nobody installed — a composition that never started has no trace of
// its own in a collector it never reached — and nothing for a deployment that named
// no address to send it to.
func (p *telemetryPlan) flush(ctx context.Context) error {
	if p == nil || p.shutdown == nil {
		return nil
	}
	return p.shutdown(ctx)
}

// installTelemetry decides, refuses and installs in one call. New and Start use the
// two halves separately, because only a boot can say when it has stopped being
// refusable; this is the whole sequence for a caller that has no boot and wants the
// answer anyway, which is every test of what a collector answered.
func installTelemetry(ctx context.Context, cfg config.Telemetry, log *slog.Logger) (func(context.Context) error, health.Report, error) {
	plan, report, err := planTelemetry(ctx, cfg, log)
	if err != nil {
		return nil, nil, err
	}
	plan.install(ctx)
	return plan.shutdown, report, nil
}

// resource is the trace's and the metric's origin: service.name always, and
// pkit.client when the deployment named one. The SDK's own attributes and whatever
// OTEL_RESOURCE_ATTRIBUTES says stay; this overrides only the name — except for the
// two keys a tenant owns, which are refused below.
//
// No tenant, and no client either when the installation is shared. A resource
// describes the process, and this process serves many tenants — so the tenant
// arrives on the spans, where a request put it, and not here. kit/telemetry's
// package comment carries the argument; this is where it is enforced, against the
// environment as well as against this file.
//
// The environment is the part that needs saying. Both providers merge
// resource.Environment() into whatever they are handed — sdk/trace's and
// sdk/metric's WithResource both do it inside the option — so a tenant key in
// OTEL_RESOURCE_ATTRIBUTES reaches every span and number this process exports no
// matter what this function builds, and the only way to hold the promise with the
// SDK as it is pinned would be to edit this process's environment behind its owner.
// So a reserved key named at boot stops the boot, the way a value of
// telemetry.otlp_endpoint that no exporter can use stops it: a configuration that
// would attribute one tenant's data to a process that serves many is wrong in a way
// no operator should discover in the backend, and the message says which key and what
// to do instead. Every other key the environment names — a deployment environment, a
// cluster, a pod — arrives unchanged.
func resource(cfg config.Telemetry) (*sdkresource.Resource, error) {
	attrs := []attribute.KeyValue{attribute.String("service.name", cfg.ServiceName)}
	if cfg.Client != "" {
		attrs = append(attrs, attribute.String(telemetry.AttrClient, cfg.Client))
	}
	if named := reservedTenantKeys(sdkresource.Environment()); len(named) > 0 {
		return nil, fmt.Errorf("app: telemetry: %s in OTEL_RESOURCE_ATTRIBUTES names a tenant on a process that "+
			"serves many tenants; remove it and let each request name its own tenant on the span it opens",
			strings.Join(named, ", "))
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

// reservedTenantKey reports whether a key is a tenant's: it belongs to the operation
// that resolved the tenant and to no process. These two keys, and only these, are
// refused on a resource — dropping a key that is not a tenant's would be this file
// deciding what an operator's deployment may say about itself.
func reservedTenantKey(key attribute.Key) bool {
	return key == telemetry.AttrTenant || key == telemetry.AttrTenantID
}

// reservedTenantKeys names the reserved keys a resource carries, which is the
// refusal's list of what to remove. An empty answer is the common one: an environment
// that asks for no tenant label needs no saying.
func reservedTenantKeys(res *sdkresource.Resource) []string {
	var keys []string
	for _, kv := range res.Attributes() {
		if reservedTenantKey(kv.Key) {
			keys = append(keys, string(kv.Key))
		}
	}
	return keys
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
