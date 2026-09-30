package telemetry_test

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/telemetry"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// collect returns the instruments a fresh provider recorded, by name. Each case
// builds its own provider rather than installing one globally: the global meter
// delegates exactly once per process, so a test that wrote it would decide what
// every other test in this binary measures. The kernel records on the global, and
// kit/telemetry's own instruments are made by the same constructor a caller passes
// its own meter to — which is what makes the three testable at all.
func collect(t *testing.T, record func(in telemetry.Instruments)) map[string]metricdata.Metrics {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	record(telemetry.NewInstruments(provider.Meter(telemetry.Scope)))
	var got metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &got); err != nil {
		t.Fatalf("collect: %v", err)
	}
	byName := map[string]metricdata.Metrics{}
	for _, scope := range got.ScopeMetrics {
		for _, m := range scope.Metrics {
			byName[m.Name] = m
		}
	}
	return byName
}

// The three instruments the brief promises, each with a tenant on its datapoint.
// A number without the tenant dimension is the number this runtime does not
// promise: one process serves many tenants, and an aggregate that cannot be split
// by tenant cannot say whose queue is stuck or whose requests are slow.
func TestTheThreeInstrumentsRecordATenantDimension(t *testing.T) {
	tenant := tenancy.Tenant{ID: uuid.MustParse("11111111-1111-4111-8111-111111111111"), Slug: "academy"}
	ctx := tenancy.WithTenant(context.Background(), tenant)
	attrs := telemetry.SpanAttrs(ctx)

	got := collect(t, func(in telemetry.Instruments) {
		in.ObserveOperation(ctx, 0.4, attrs...)
		in.ObserveOutboxLag(ctx, 3, attrs...)
		in.CountRefusal(ctx, "forbidden", attrs...)
	})

	for _, name := range []string{
		"pkit.http.operation.duration", "pkit.outbox.lag", "pkit.http.refusals",
	} {
		m, ok := got[name]
		if !ok {
			t.Errorf("no %s instrument was collected; got %v", name, keys(got))
			continue
		}
		if m.Unit == "" {
			t.Errorf("%s carries no unit", name)
		}
	}

	dur, ok := got["pkit.http.operation.duration"].Data.(metricdata.Histogram[float64])
	if !ok || len(dur.DataPoints) != 1 {
		t.Fatalf("operation duration is %#v, want one histogram datapoint", got["pkit.http.operation.duration"].Data)
	}
	if dur.DataPoints[0].Count != 1 || dur.DataPoints[0].Sum != 0.4 {
		t.Errorf("operation duration counted %d samples summing %v, want 1 sample of 0.4",
			dur.DataPoints[0].Count, dur.DataPoints[0].Sum)
	}
	wantAttrs(t, dur.DataPoints[0].Attributes, map[string]string{
		telemetry.AttrTenant: "academy", telemetry.AttrTenantID: tenant.ID.String(),
	})

	lag, ok := got["pkit.outbox.lag"].Data.(metricdata.Gauge[float64])
	if !ok || len(lag.DataPoints) != 1 {
		t.Fatalf("outbox lag is %#v, want one gauge datapoint", got["pkit.outbox.lag"].Data)
	}
	wantAttrs(t, lag.DataPoints[0].Attributes, map[string]string{telemetry.AttrTenantID: tenant.ID.String()})

	refused, ok := got["pkit.http.refusals"].Data.(metricdata.Sum[int64])
	if !ok || len(refused.DataPoints) != 1 {
		t.Fatalf("refusals is %#v, want one sum datapoint", got["pkit.http.refusals"].Data)
	}
	if refused.DataPoints[0].Value != 1 {
		t.Errorf("one refusal counted %d, want 1", refused.DataPoints[0].Value)
	}
	wantAttrs(t, refused.DataPoints[0].Attributes, map[string]string{
		telemetry.AttrTenant: "academy", telemetry.AttrRefusalClass: "forbidden",
	})
}

// A span attribute set with no tenant and no request in it is empty rather than
// carrying an empty string under each key: an attribute that reads "" is a tenant
// named nothing, which is a claim and not an absence.
func TestSpanAttrsWritesOnlyWhatTheContextKnows(t *testing.T) {
	if got := telemetry.SpanAttrs(context.Background()); len(got) != 0 {
		t.Errorf("an empty context produced %v, want no attributes", got)
	}
	id := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	// Only an id: the shape of a delivery, which holds the id its event names and
	// has no slug to name it with.
	ctx := tenancy.WithTenant(context.Background(), tenancy.Tenant{ID: id})
	got := telemetry.SpanAttrs(ctx)
	if len(got) != 1 || string(got[0].Key) != telemetry.AttrTenantID {
		t.Fatalf("%v, want only %s", got, telemetry.AttrTenantID)
	}
	if got[0].Value.AsString() != id.String() {
		t.Errorf("%s = %s, want %s", telemetry.AttrTenantID, got[0].Value.Emit(), id)
	}
}

// The request id travels as baggage, which is what lets a span opened three calls
// below — a database transaction, a tenant's share of a job — name the request that
// caused it without the id being threaded through every signature.
func TestTheRequestIDTravelsAsBaggage(t *testing.T) {
	const id = "req-7f3a"
	ctx := telemetry.WithRequestID(context.Background(), id)
	if got := telemetry.RequestID(ctx); got != id {
		t.Errorf("RequestID = %q, want %q", got, id)
	}
	// An empty id is not carried as an empty member: an attribute that reads "" is
	// a request named nothing, which is a claim and not an absence.
	if got := telemetry.RequestID(telemetry.WithRequestID(context.Background(), "academy/collect")); got != "academy/collect" {
		t.Errorf("RequestID = %q, want the id carried unchanged", got)
	}
	if got := telemetry.RequestID(telemetry.WithRequestID(context.Background(), "")); got != "" {
		t.Errorf("an empty id was carried as %q, want nothing carried", got)
	}
	if attrs := telemetry.SpanAttrs(ctx); len(attrs) != 1 || string(attrs[0].Key) != telemetry.AttrRequestID {
		t.Errorf("SpanAttrs = %v, want only the request id", attrs)
	}
}

// The class set is closed. Thirteen classes, so a new reason for refusing a request
// arrives in one of them and never as a string invented at a call site, which is
// what would make the counter unreadable a month later. The statuses kit/httpx
// answers on purpose — including 402, the plan gate — are in it, which is what
// TestEveryRefusalThisKernelWritesHasAClassOfItsOwn pins from the call sites.
func TestRefusalClassIsAClosedSetOverTheStatus(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   string
	}{
		{400, "invalid"}, {422, "invalid"}, {401, "unauthenticated"}, {403, "forbidden"},
		{402, "plan_excluded"}, {404, "not_found"}, {405, "not_allowed"}, {409, "conflict"},
		{413, "too_large"},
		{429, "rate_limited"}, {408, "timeout"}, {504, "timeout"}, {503, "unavailable"},
		{500, "failed"}, {502, "unavailable"}, {0, "unknown"}, {399, "unknown"},
	} {
		if got := telemetry.RefusalClass(tc.status); got != tc.want {
			t.Errorf("RefusalClass(%d) = %q, want %q", tc.status, got, tc.want)
		}
	}
}

// The scope is the name a trace backend shows for every span and meter the kernel
// makes, so it is one constant rather than four spellings.
func TestScopeNamesThisRepository(t *testing.T) {
	if telemetry.Scope != "github.com/septagon-oss/platformkit/kit/telemetry" {
		t.Errorf("scope is %q", telemetry.Scope)
	}
	if telemetry.Propagators().Fields() == nil {
		t.Error("Propagators answer no fields; a process that propagates nothing propagates no trace")
	}
	// The four keys, spelled once: a key spelled twice is a key no filter answers.
	for _, k := range []string{telemetry.AttrTenant, telemetry.AttrTenantID, telemetry.AttrClient, telemetry.AttrRequestID} {
		if len(k) < 6 || k[:5] != "pkit." {
			t.Errorf("attribute key %q is not namespaced under pkit.", k)
		}
	}
}

func wantAttrs(t *testing.T, set attribute.Set, want map[string]string) {
	t.Helper()
	for k, v := range want {
		got, ok := set.Value(attribute.Key(k))
		if !ok {
			t.Errorf("datapoint has no %s attribute; it has %v", k, set.ToSlice())
			continue
		}
		if got.AsString() != v {
			t.Errorf("%s = %s, want %q", k, got.Emit(), v)
		}
	}
}

func keys(m map[string]metricdata.Metrics) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
