package app

// The pinned SDK merges OTEL_RESOURCE_ATTRIBUTES into whatever Resource a provider
// is handed — sdk/trace's and sdk/metric's WithResource both call
// resource.Merge(resource.Environment(), r) inside the option — so the environment
// describes this process whether or not this composition asked it to. Two promises
// live side by side there, and only one of them was pinned: a tenant key named at
// boot stops the boot (TestASecondTenantHasNoFirstTenantResourceAttributes), and
// nothing *else* the environment names does. The second is the promise a cure could
// break by accident: refusing the variable wholesale, or reading it and dropping
// everything in it, keeps the tenant out of the resource and takes the operator's
// own description of where the process runs out with it. So each case below boots a
// child with a different value in a fresh process — the SDK reads the environment
// once and caches what it found — and the child answers for the process it is in:
// what its exported span's resource says, or what the refusal named.

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/telemetry"
)

func TestTheEnvironmentDescribesTheProcessAndNeverATenant(t *testing.T) {
	const child = "PKIT_RESOURCE_ENVIRONMENT_CHILD"
	const honest = "deployment.environment.name=staging"

	if mode := os.Getenv(child); mode != "" {
		res, err := resource(config.Telemetry{ServiceName: "platformkit"})
		if mode == "describes" {
			if err != nil {
				t.Fatalf("an environment that names no tenant refused the boot: %v", err)
			}
			exporter := tracetest.NewInMemoryExporter()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter), sdktrace.WithResource(res))
			defer provider.Shutdown(t.Context())
			_, span := provider.Tracer("environment").Start(t.Context(), "operation")
			span.End()
			have := map[string]string{}
			for _, attr := range res.Attributes() {
				have[string(attr.Key)] = attr.Value.Emit()
			}
			for _, want := range [][2]string{
				{"service.name", "platformkit"},
				{"deployment.environment.name", "staging"},
			} {
				got, found := have[want[0]]
				if !found {
					t.Errorf("the process's resource carries no %s, only %v", want[0], have)
				} else if got != want[1] {
					t.Errorf("the process's resource carries %s=%q, want %q", want[0], got, want[1])
				}
			}
			spans := exporter.GetSpans()
			if len(spans) != 1 {
				t.Fatalf("exported %d spans, want the one operation", len(spans))
			}
			have = map[string]string{}
			for _, attr := range spans[0].Resource.Attributes() {
				have[string(attr.Key)] = attr.Value.Emit()
			}
			for _, want := range [][2]string{
				{"service.name", "platformkit"},
				{"deployment.environment.name", "staging"},
			} {
				got, found := have[want[0]]
				if !found {
					t.Errorf("the exported span's resource carries no %s, only %v", want[0], have)
				} else if got != want[1] {
					t.Errorf("the exported span's resource carries %s=%q, want %q", want[0], got, want[1])
				}
			}
			return
		}
		if err == nil {
			t.Fatal("an environment naming a tenant booted: this process would answer for one tenant and serve many")
		}
		if !strings.Contains(strings.ToLower(err.Error()), "tenant") {
			t.Errorf("the refusal does not say a tenant is why: %v", err)
		}
		for _, key := range []string{telemetry.AttrTenant, telemetry.AttrTenantID} {
			if !strings.Contains(err.Error(), key) {
				t.Errorf("the refusal does not name %s, so nobody knows what to remove: %v", key, err)
			}
		}
		if strings.Contains(err.Error(), "deployment.environment.name") {
			t.Errorf("the refusal objects to an attribute that names no tenant: %v", err)
		}
		return
	}

	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("locating test executable: %v", err)
	}
	for _, tc := range []struct {
		what  string
		mode  string
		attrs string
	}{
		{what: "an environment that describes only the deployment", mode: "describes", attrs: honest},
		{what: "an environment that names a tenant beside the deployment", mode: "refuses",
			attrs: honest + ",pkit.tenant=first,pkit.tenant.id=11111111-1111-4111-8111-111111111111"},
	} {
		t.Run(tc.what, func(t *testing.T) {
			cmd := exec.CommandContext(t.Context(), executable, "-test.run=^TestTheEnvironmentDescribesTheProcessAndNeverATenant$")
			cmd.Env = append(os.Environ(), child+"="+tc.mode, "OTEL_RESOURCE_ATTRIBUTES="+tc.attrs)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Errorf("%v\n%s", err, strings.TrimSpace(string(out)))
			}
		})
	}
}
