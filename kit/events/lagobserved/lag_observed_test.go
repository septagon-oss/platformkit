// Package lagobserved reads the queue's own number back off a meter provider of its own.
//
// It is a package of its own because of how OpenTelemetry binds meters: the instruments
// kit/telemetry shares were taken from the global meter, and the global binds to the
// provider installed *first* in a process. kit/events/review_round1_outbox_lag_test.go
// installs the reader of that test binary, so no second case in that binary can collect
// what the relay records — the delivery's own relay_lag_test.go says so, and states that
// the value's path to a reader is therefore untested there. This binary gets its own
// first provider, which is the only way to ask the gauge what it says about a queue.
package lagobserved_test

import (
	"testing"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/telemetry"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// TestADrainedQueueLeavesTheLagGaugeBehind: the queue's number says how far behind the
// queue is *now*, which is the promise relay.go prints and the CHANGELOG repeats ("the
// question is 'how far behind is the queue now'").
//
// A row five minutes old is relayed, the gauge rightly reads 300 seconds, and the queue is
// then empty. The next collection must not still read 300: a series that reports a
// backlog which has drained is a number an operator would page on for as long as nobody
// publishes that event name again, and the relay records the series only when it has a row
// for it, so a drained series is never corrected. What the fix can do — record zero for the
// series a drained pass last touched, or report the gauge with delta temporality so a
// silent series disappears — is the delivery's; what both answer is the assertion below.
//
// The case is about this branch's own instrument, and only this branch writes it.
func TestADrainedQueueLeavesTheLagGaugeBehind(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))

	admin, conn := dbtest.Schema(t)
	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}
	const name = "billing.invoice_issued"
	_, err := admin.ExecContext(t.Context(),
		`INSERT INTO platformkit_outbox (id, tenant_id, name, payload, created_at)
		 VALUES ($1, $2, $3, '{}'::jsonb, now() - interval '5 minutes')`,
		uuid.New(), tenant.ID, name)
	if err != nil {
		t.Fatalf("insert a row that has been waiting five minutes: %v", err)
	}
	if err := events.Relay(t.Context(), conn, memory.New()); err != nil {
		t.Fatalf("relay it: %v", err)
	}
	if value, _, held := lag(t, reader, tenant.ID); !held {
		t.Fatalf("the pass that moved the row recorded no pkit.outbox.lag for tenant %s", tenant.ID)
	} else if value < 290 || value > 310 {
		t.Fatalf("the gauge reads %.1f s about a row five minutes old, want about 300", value)
	}

	// The queue is empty. A pass finds nothing, records nothing, and the reading stays.
	if err := events.Relay(t.Context(), conn, memory.New()); err != nil {
		t.Fatalf("relay the empty queue: %v", err)
	}
	value, attrs, held := lag(t, reader, tenant.ID)
	if held && value > 1 {
		t.Errorf("after the queue drained, pkit.outbox.lag still reads %.1f s for %v; the gauge is written "+
			"only when the relay has a row for the series, so the reading of a backlog that drained is kept "+
			"until that same tenant publishes that same event name again — which for a name published once a "+
			"day is the next day. The number says how far behind the queue is now, and now the queue is not "+
			"behind at all", value, attrs)
	}
}

// lag is the one datapoint of pkit.outbox.lag that names this tenant, and whether there
// was one at all.
func lag(t *testing.T, reader *sdkmetric.ManualReader, tenantID uuid.UUID) (float64, []attribute.KeyValue, bool) {
	t.Helper()
	var out metricdata.ResourceMetrics
	if err := reader.Collect(t.Context(), &out); err != nil {
		t.Fatalf("collect: %v", err)
	}
	for _, scope := range out.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "pkit.outbox.lag" {
				continue
			}
			g, ok := m.Data.(metricdata.Gauge[float64])
			if !ok {
				t.Fatalf("pkit.outbox.lag carries %T, want a gauge", m.Data)
			}
			for _, p := range g.DataPoints {
				if id, has := p.Attributes.Value(attribute.Key(telemetry.AttrTenantID)); has &&
					id.AsString() == tenantID.String() {
					return p.Value, p.Attributes.ToSlice(), true
				}
			}
		}
	}
	return 0, nil, false
}
