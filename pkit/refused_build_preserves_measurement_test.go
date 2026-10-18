package pkit_test

import (
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/pkit"
)

func TestARefusedBuildPreservesMetersAndPropagation(t *testing.T) {
	// The SDK's first-provider delegation is process state, so use the same
	// subprocess boundary as the tracer lifecycle case.
	const child = "PKIT_REFUSED_BUILD_MEASUREMENT_CHILD"
	if os.Getenv(child) == "" {
		executable, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.CommandContext(t.Context(), executable, "-test.run=^TestARefusedBuildPreservesMetersAndPropagation$", "-test.timeout=2m", "-test.v")
		cmd.Env = append(os.Environ(), child+"=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("isolated measurement lifecycle: %v\n%s", err, out)
		}
		return
	}
	for _, stage := range []string{"route", "route_without_export", "transport"} {
		t.Run(stage, func(t *testing.T) {
			reader := sdkmetric.NewManualReader()
			serving := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
			otel.SetMeterProvider(serving)
			defer serving.Shutdown(t.Context())
			otel.SetTextMapPropagator(propagation.Baggage{})
			before, err := otel.Meter("serving").Int64Counter("operations")
			if err != nil {
				t.Fatal(err)
			}
			before.Add(t.Context(), 1)
			cfg := onOneDatabase(t)
			if stage != "route_without_export" {
				cfg.Telemetry.OTLPEndpoint = "http://127.0.0.1:1"
			}
			deployment := buildDeployment(cfg, app.All)
			modules := []*pkit.Module{doors, ledger}
			reason := "ledger:read"
			if stage == "transport" {
				modules = []*pkit.Module{doors, desk}
				deployment.Transports.Memory = func() events.Transport { return nil }
				reason = "transport"
			}
			runtime, err := pkit.NewApp("collect").Use(modules...).Build(t.Context(), deployment)
			if runtime != nil {
				defer runtime.Close()
				t.Error("refused composition returned a runtime")
			}
			if err == nil || !strings.Contains(err.Error(), reason) {
				t.Fatalf("expected %s refusal: %v", reason, err)
			}
			if stage != "transport" && tablesIn(t, cfg.Database.MigrateURL) != 0 {
				t.Error("structural refusal migrated the database")
			}
			if otel.GetMeterProvider() != serving {
				t.Error("refused build replaced the serving meter provider")
			}
			if got := otel.GetTextMapPropagator().Fields(); !slices.Equal(got, []string{"baggage"}) {
				t.Errorf("refused build changed propagation fields to %v", got)
			}
			after, err := otel.Meter("serving").Int64Counter("operations")
			if err != nil {
				t.Fatal(err)
			}
			after.Add(t.Context(), 1)
			var data metricdata.ResourceMetrics
			if err := reader.Collect(t.Context(), &data); err != nil {
				t.Fatal(err)
			}
			var total int64
			for _, scope := range data.ScopeMetrics {
				for _, measurement := range scope.Metrics {
					if sum, ok := measurement.Data.(metricdata.Sum[int64]); ok && measurement.Name == "operations" {
						for _, point := range sum.DataPoints {
							total += point.Value
						}
					}
				}
			}
			if total != 2 {
				t.Errorf("serving recorder received %d operations, want both operations", total)
			}
		})
	}
}
