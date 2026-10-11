package pkit_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/pkit"
)

func TestARefusedBuildPreservesTheServingTracer(t *testing.T) {
	// Provider installation changes process globals; keep this case independent
	// of other applications and the SDK's first-provider delegation.
	const child = "PKIT_REFUSED_BUILD_TELEMETRY_CHILD"
	if os.Getenv(child) == "" {
		executable, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.CommandContext(t.Context(), executable, "-test.run=^TestARefusedBuildPreservesTheServingTracer$", "-test.timeout=2m", "-test.v")
		cmd.Env = append(os.Environ(), child+"=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("isolated telemetry lifecycle: %v\n%s", err, out)
		}
		return
	}
	exporter := tracetest.NewInMemoryExporter()
	serving := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	otel.SetTracerProvider(serving)
	defer serving.Shutdown(t.Context())
	_, before := otel.Tracer("serving").Start(t.Context(), "before refused build")
	before.End()
	if len(exporter.GetSpans()) != 1 {
		t.Fatal("the serving recorder did not receive its initial span")
	}
	cfg := onOneDatabase(t)
	cfg.Telemetry.OTLPEndpoint = "http://127.0.0.1:1"
	cfg.Telemetry.ServiceName = "refused-composition"
	runtime, err := pkit.NewApp("collect").Use(doors, ledger).Build(t.Context(), buildDeployment(cfg, app.All))
	if runtime != nil {
		defer runtime.Close()
		t.Error("a composition using an undefined permission returned a runtime")
	}
	if err == nil || !strings.Contains(err.Error(), "ledger:read") {
		t.Fatalf("expected refusal naming the undefined permission: %v", err)
	}
	if got := tablesIn(t, cfg.Database.MigrateURL); got != 0 {
		t.Errorf("refused composition migrated %d tables", got)
	}
	if otel.GetTracerProvider() != serving {
		t.Error("a refused Build replaced the serving process's tracer provider")
	}
	_, after := otel.Tracer("serving").Start(t.Context(), "after refused build")
	after.End()
	if got := len(exporter.GetSpans()); got != 2 {
		t.Errorf("serving recorder received %d spans, want both operations; the refused Build diverted subsequent telemetry", got)
	}
}
