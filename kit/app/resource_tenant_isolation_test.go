package app

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/telemetry"
)

func TestASecondTenantHasNoFirstTenantResourceAttributes(t *testing.T) {
	const helper = "PKIT_RESOURCE_TENANT_TEST_HELPER"
	if os.Getenv(helper) == "1" {
		res, err := resource(config.Telemetry{ServiceName: "platformkit"})
		if err != nil {
			if !strings.Contains(strings.ToLower(err.Error()), "tenant") {
				t.Errorf("resource refused the tenant attribute without naming why: %v", err)
			}
			return
		}
		var named bool
		for _, attr := range res.Attributes() {
			switch string(attr.Key) {
			case "service.name":
				named = attr.Value.AsString() == "platformkit"
			case telemetry.AttrTenant, telemetry.AttrTenantID:
				t.Errorf("process resource carries %s=%q; one process serves more than one tenant", attr.Key, attr.Value.AsString())
			}
		}
		if !named {
			t.Error("resource did not name the configured service")
		}

		exporter := tracetest.NewInMemoryExporter()
		provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter), sdktrace.WithResource(res))
		defer provider.Shutdown(t.Context())
		_, span := provider.Tracer("tenant-boundary").Start(t.Context(), "operation",
			trace.WithAttributes(attribute.String(telemetry.AttrTenant, "second")))
		span.End()
		spans := exporter.GetSpans()
		if len(spans) != 1 {
			t.Fatalf("exported %d spans, want the second tenant's operation", len(spans))
		}
		var second bool
		for _, attr := range spans[0].Attributes {
			if string(attr.Key) == telemetry.AttrTenant && attr.Value.AsString() == "second" {
				second = true
			}
		}
		if !second {
			t.Fatal("the second tenant's operation did not reach the trace exporter")
		}
		for _, attr := range spans[0].Resource.Attributes() {
			if string(attr.Key) == telemetry.AttrTenant || string(attr.Key) == telemetry.AttrTenantID {
				t.Errorf("second tenant's span carries process resource %s=%q", attr.Key, attr.Value.AsString())
			}
		}

		reader := sdkmetric.NewManualReader()
		meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader), sdkmetric.WithResource(res))
		defer meterProvider.Shutdown(t.Context())
		instruments := telemetry.NewInstruments(meterProvider.Meter("tenant-boundary"))
		instruments.ObserveOperation(t.Context(), 0.1, attribute.String(telemetry.AttrTenant, "second"))
		var numbers metricdata.ResourceMetrics
		if err := reader.Collect(t.Context(), &numbers); err != nil {
			t.Fatalf("collecting the second tenant's operation: %v", err)
		}
		if len(numbers.ScopeMetrics) == 0 {
			t.Fatal("the second tenant's operation did not reach the metric reader")
		}
		for _, attr := range numbers.Resource.Attributes() {
			if string(attr.Key) == telemetry.AttrTenant || string(attr.Key) == telemetry.AttrTenantID {
				t.Errorf("second tenant's number carries process resource %s=%q", attr.Key, attr.Value.AsString())
			}
		}
		return
	}

	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("locating test executable: %v", err)
	}
	cmd := exec.CommandContext(t.Context(), executable, "-test.run=^TestASecondTenantHasNoFirstTenantResourceAttributes$")
	cmd.Env = append(os.Environ(),
		helper+"=1",
		"OTEL_RESOURCE_ATTRIBUTES=pkit.tenant=first,pkit.tenant.id=first-id",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Errorf("a tenant attribute supplied at process start reached the resource: %v\n%s", err, strings.TrimSpace(string(out)))
	}
}
