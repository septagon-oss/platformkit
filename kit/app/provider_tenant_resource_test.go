package app

import (
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"

	"github.com/septagon-oss/platformkit/kit/config"
)

func TestATenantLabelRefusesProviderInstallation(t *testing.T) {
	const child = "PKIT_PROVIDER_TENANT_RESOURCE_CHILD"
	if os.Getenv(child) == "1" {
		beforeTraces := otel.GetTracerProvider()
		beforeMetrics := otel.GetMeterProvider()
		shutdown, report, err := installTelemetry(t.Context(), config.Telemetry{
			ServiceName:  "platformkit",
			OTLPEndpoint: "127.0.0.1:4317",
		}, slog.New(slog.NewTextHandler(io.Discard, nil)))
		if err == nil {
			t.Fatal("a tenant-labelled process installed exporters for all tenants")
		}
		for _, want := range []string{"pkit.tenant", "pkit.tenant.id", "remove"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("provider refusal %q does not explain %q", err, want)
			}
		}
		if shutdown != nil || report != nil {
			t.Error("a refused installation returned an exporter or its health report")
		}
		if otel.GetTracerProvider() != beforeTraces || otel.GetMeterProvider() != beforeMetrics {
			t.Error("a refused installation changed a process-wide provider")
		}
		return
	}

	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("locating test executable: %v", err)
	}
	cmd := exec.CommandContext(t.Context(), executable, "-test.run=^TestATenantLabelRefusesProviderInstallation$")
	cmd.Env = append(os.Environ(), child+"=1", "OTEL_RESOURCE_ATTRIBUTES=pkit.tenant=first,pkit.tenant.id=first-id")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Errorf("tenant-labelled provider installation: %v\n%s", err, strings.TrimSpace(string(out)))
	}
}
