package events

// Which row of a batch answers for its series. The relay's own pass is covered by
// kit/events' relay tests and the datapoint's attribute set by
// outbox_lag_test.go; what neither can reach is the choice the gauge
// turns into a reading, because one binary gets one meter provider and that reader
// belongs to the review's case. So the choice is a function of its own and is tested
// here, over the rows a relay pass actually reads.

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestOneRelayPassRecordsTheOldestRowOfEachSeries(t *testing.T) {
	acme, other := uuid.New(), uuid.New()
	old := time.Now().Add(-5 * time.Minute)
	middle := time.Now().Add(-2 * time.Minute)
	fresh := time.Now().Add(-10 * time.Second)

	// The order the relay reads its batch in: ORDER BY created_at, id. Three rows of
	// one event name for one tenant, then a second tenant's row of the same name, then
	// another event of the tenant's — three series, five rows.
	rows := []row{
		{ID: uuid.MustParse("11111111-1111-1111-1111-111111111111"), TenantID: acme, Name: "billing.invoice_issued", CreatedAt: old},
		{ID: uuid.MustParse("22222222-2222-2222-2222-222222222222"), TenantID: acme, Name: "billing.invoice_issued", CreatedAt: middle},
		{ID: uuid.MustParse("33333333-3333-3333-3333-333333333333"), TenantID: acme, Name: "billing.invoice_issued", CreatedAt: fresh},
		{ID: uuid.MustParse("44444444-4444-4444-4444-444444444444"), TenantID: other, Name: "billing.invoice_issued", CreatedAt: middle},
		{ID: uuid.MustParse("55555555-5555-5555-5555-555555555555"), TenantID: acme, Name: "user.invited", CreatedAt: fresh},
	}

	var recorded []row
	oldestPerSeries(rows, func(r row) { recorded = append(recorded, r) })

	if len(recorded) != 3 {
		t.Fatalf("a pass over five rows in three series recorded %d lag values, want one per series: %v",
			len(recorded), recorded)
	}
	// One value per series is half of it; which row carries it is the other half. A
	// gauge keeps the value written last, so a series whose second or third row was
	// recorded reports a shorter wait than the queue is actually holding — the
	// reading that says a backlog is draining at the moment it is furthest behind.
	want := map[uuid.UUID]row{}
	for _, r := range rows {
		if _, seen := want[r.ID]; !seen {
			want[r.ID] = r
		}
	}
	oldest := map[uuid.UUID]bool{
		rows[0].ID: true, // acme / invoice_issued, the five-minute row
		rows[3].ID: true, // the other tenant's row of that name
		rows[4].ID: true, // acme / user.invited
	}
	for _, r := range recorded {
		if !oldest[r.ID] {
			t.Errorf("the series %s/%s recorded row %s, created %v, when the batch also held its older "+
				"rows: the gauge keeps the last value written, so recording the newer row reports the "+
				"shortest wait in the batch as the queue's wait", r.TenantID, r.Name, r.ID,
				r.CreatedAt.Sub(rows[0].CreatedAt).Round(time.Second))
		}
	}
}
