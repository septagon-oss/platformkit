package events_test

// The brief's headline is "the tenant on every number", and
// one of the three instruments it names is the outbox lag gauge. kit/telemetry's test
// checks the gauge carries a tenant when kit/telemetry is handed one; nothing checked
// that the code that actually produces the number — relayBatch — attaches one. This
// case runs a real relay pass over a real outbox row and asks the collected gauge
// whose tenant its datapoint names.
//
// It is a pin, not a complaint: it passes today. It is here because the claim in
// kit/events/relay.go ("Recorded per event and per tenant, because a lag that cannot
// be attributed to a tenant cannot tell you whose events are stuck") is a claim about
// the attribute set of a datapoint, and an attribute set is only a claim until a case
// reads the datapoint back.

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/telemetry"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func TestTheRelayRecordsTheLagOfARowForTheTenantThatOwnsIt(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))

	_, conn := dbtest.Schema(t)
	const name = "billing.invoice_issued"
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return events.Publish(ctx, tx, name, map[string]any{"amount": 1})
	})
	if err != nil {
		t.Fatalf("publish the row the relay will read: %v", err)
	}
	if err := events.Relay(t.Context(), conn, memory.New()); err != nil {
		t.Fatalf("relay it: %v", err)
	}

	var out metricdata.ResourceMetrics
	if err := reader.Collect(t.Context(), &out); err != nil {
		t.Fatalf("collect: %v", err)
	}
	var lag *metricdata.Gauge[float64]
	for _, scope := range out.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "pkit.outbox.lag" {
				continue
			}
			g, ok := m.Data.(metricdata.Gauge[float64])
			if !ok {
				t.Fatalf("pkit.outbox.lag is %T, want a gauge", m.Data)
			}
			lag = &g
		}
	}
	if lag == nil {
		t.Fatal("a relay pass moved a row and no pkit.outbox.lag was collected at all")
	}
	for _, p := range lag.DataPoints {
		id, has := p.Attributes.Value(attribute.Key(telemetry.AttrTenantID))
		if !has {
			t.Errorf("the lag of a tenant's row carries no %s: %v",
				telemetry.AttrTenantID, p.Attributes.ToSlice())
			continue
		}
		if id.AsString() == acme.ID.String() {
			return
		}
	}
	t.Errorf("no pkit.outbox.lag datapoint names tenant %s; a queue that cannot say whose events "+
		"are stuck is the number the brief refuses to ship (%d datapoints collected)",
		acme.ID, len(lag.DataPoints))
}
