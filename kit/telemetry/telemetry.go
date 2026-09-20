// Package telemetry decides what the kernel does with the spans it makes.
//
// It is one function because the decision is one: where spans go, and at what
// rate. kit/httpx, kit/events and kit/jobs each make spans through the global
// tracer provider and never import this package; this is the only place that
// chooses a provider, an exporter and a sampler, so there is one thing to read
// when a deployment asks "where did my traces go?".
//
// The global is deliberate and it is the reason Start exists at all. A tracer
// that had to be passed to every middleware, handler and job would be a
// parameter in half the kernel's signatures, and OpenTelemetry's own
// instrumentation — otelhttp, and any library a module later adopts — reads the
// global provider and the global propagators and nothing else. Start writes
// both, once, at the boot, and the shutdown it hands back flushes what is
// buffered. Nothing else may call otel.SetTracerProvider, and one process calls
// Start exactly once — which is what kit/app's New is. The reason it is once is
// in OpenTelemetry itself: a Tracer handed out before the first install stays
// bound to the provider that first install named, and a later install changes
// only what otel.GetTracerProvider answers and the tracers made after it.
// kit/events and kit/jobs made theirs at initialization, so a second Start would
// leave them reporting to the first. A test that reads spans therefore installs
// its recorder before any package tracer exists — once per binary, not per case.
//
// Tracing is off unless telemetry.otlp_endpoint names a collector. The
// alternative — record everything and let the collector decide — makes a
// deployment without a backend pay for spans it will never read, and makes the
// decision belong to something outside the composition.
package telemetry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	sdkresource "go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/septagon-oss/platformkit/kit/config"
)

// Start installs the process's tracer and returns the function that flushes it.
//
// The propagators are installed whatever the endpoint says: an untraced process
// still receives requests that carry a trace parent, and still publishes events
// that a traced process reads back. Dropping the context because this process
// records nothing is what makes a trace stop at a service boundary, which is the
// failure tracing exists to remove.
//
// An endpoint that is not an OTLP/HTTP URL is refused before anything is
// installed, so a typo cannot leave the process half-traced. A collector that
// is unreachable is not: the exporter buffers, warns through OTel's own error
// handler and keeps serving, because tracing must never be the reason an
// application does not start.
func Start(ctx context.Context, cfg config.Telemetry, log *slog.Logger) (shutdown func(context.Context) error, err error) {
	if log == nil {
		log = slog.Default()
	}
	otel.SetTextMapPropagator(Propagators())

	if cfg.OTLPEndpoint == "" {
		otel.SetTracerProvider(noop.NewTracerProvider())
		log.DebugContext(ctx, "telemetry: tracing is off; telemetry.otlp_endpoint is empty")
		return func(context.Context) error { return nil }, nil
	}
	endpoint, err := endpoint(cfg.OTLPEndpoint)
	if err != nil {
		return nil, err
	}
	exporter, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(cfg.OTLPEndpoint))
	if err != nil {
		return nil, fmt.Errorf("telemetry: %w", err)
	}
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		// ParentBased, so a sampled request stays one trace through the outbox
		// and the worker: the ratio is the decision for a trace this process
		// starts, never a veto on one it was handed.
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.Ratio()))),
		sdktrace.WithResource(resource(cfg.ServiceName)),
	)
	otel.SetTracerProvider(provider)
	log.InfoContext(ctx, "telemetry: tracing", "endpoint", endpoint, "service", cfg.ServiceName,
		"sample_ratio", cfg.Ratio())
	return provider.Shutdown, nil
}

// Propagators is the propagation set every PlatformKit process installs: the W3C
// trace context, and the baggage beside it. It is exported because reading a
// trace context is a different thing from exporting spans: a process with no
// collector still receives requests that carry a trace parent and still leaves a
// context on the outbox row for a traced process to read, and neither is possible
// without these. A process that does not compose itself through kit/app still has
// to install the same set.
func Propagators() propagation.TextMapPropagator {
	return propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{})
}

// endpoint is the collector's host and path as one printable string, for the log
// line. The URL was parsed once, here, so what a log line carries is a host and
// a path and not whatever a typo put in the key.
func endpoint(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		(u.Scheme != "http" && u.Scheme != "https") {
		return "", errors.New("telemetry: telemetry.otlp_endpoint must be an http:// or https:// URL with a host")
	}
	return u.Host + u.Path, nil
}

// resource is the trace's origin. service.name is the one attribute a reader uses
// to tell one deployment's spans from another's; the SDK's own attributes and
// whatever OTEL_RESOURCE_ATTRIBUTES says stay, and this overrides only the name.
func resource(serviceName string) *sdkresource.Resource {
	// Schemaless, so merging with the SDK's default resource keeps that
	// resource's schema URL instead of refusing two schema URLs in one merge.
	// The key is spelled here rather than imported from a versioned semconv
	// package because service.name predates every version of it.
	name, err := sdkresource.Merge(sdkresource.Default(),
		sdkresource.NewSchemaless(attribute.String("service.name", serviceName)))
	if err != nil {
		// Only a schema-URL conflict fails, and a schemaless resource cannot
		// cause one. A resource nobody can name is worth less than a trace with
		// the SDK's own name in it, so the default wins rather than the boot.
		return sdkresource.Default()
	}
	return name
}
