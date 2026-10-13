package events

// What the drained-series read answers, and for whom.
//
// relay_drain_test.go owns the choice the correction makes (which series a pass owes a zero
// to) and a_relay_pass_drains_whatever_its_ledger_holds_test.go owns the size
// it has to survive. What is left, and what this file owns, is the answer itself — because
// the read no longer asks the queue about the series the caller named. It asks the queue
// what it holds, for every tenant, and answers the caller's question out of that. Two things
// follow that nothing else pins: that a series nobody asked about stays out of the answer,
// and that a row which has been stamped stops being a reason to keep reporting a wait.
//
// The read is cross-tenant, and that is the relay's scope and no wider one: it runs on the
// system capability the relay's own batch read uses, and its answer only chooses which series
// this process owes a zero to.

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

func TestStillWaitingAnswersOnlyTheSeriesItWasAskedAbout(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	waiting, held, published := uuid.New(), uuid.New(), uuid.New()
	names := []lagKey{
		{tenant: waiting, event: "billing.invoice_issued"},
		{tenant: held, event: "user.invited"},
		{tenant: published, event: "user.invited"},
		{tenant: uuid.New(), event: "billing.invoice_issued"}, // a row of a series nobody asked about
		{tenant: uuid.New(), event: "user.invited"},           // and a second one
	}
	seed := func(tenant uuid.UUID, name string, stamped bool) {
		t.Helper()
		stamp := "NULL"
		if stamped {
			stamp = "clock_timestamp()"
		}
		_, err := admin.ExecContext(t.Context(),
			`INSERT INTO platformkit_outbox (id, tenant_id, name, payload, created_at, published_at)
			 VALUES ($1, $2, $3, '{}'::jsonb, now() - interval '5 minutes', `+stamp+")",
			uuid.New(), tenant, name)
		if err != nil {
			t.Fatalf("seed %s for tenant %s: %v", name, tenant, err)
		}
	}
	seed(names[0].tenant, names[0].event, false)
	seed(names[1].tenant, names[1].event, false)
	seed(names[2].tenant, names[2].event, true) // relayed: the queue no longer holds it
	seed(names[3].tenant, names[3].event, false)
	seed(names[4].tenant, names[4].event, false)

	reported := map[lagKey]struct{}{names[0]: {}, names[1]: {}, names[2]: {}}
	var got map[lagKey]struct{}
	err := db.RunSystem(t.Context(), conn, relayToken, func(ctx context.Context, tx db.Tx[db.System]) error {
		var err error
		got, err = stillWaiting(ctx, tx, reported)
		return err
	})
	if err != nil {
		t.Fatalf("read the series that are still waiting: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("stillWaiting answered %v for the three series it was asked about, want the two whose rows "+
			"the queue still holds: the read now takes the queue's whole pending set and answers the question "+
			"out of that, so an answer that repeats it either forgot the caller or counts a stamped row", got)
	}
	for _, k := range []lagKey{names[0], names[1]} {
		if _, ok := got[k]; !ok {
			t.Errorf("stillWaiting left out tenant %s / %s, whose row is unpublished: a series taken to be "+
				"drained while its row waits is the number announcing a backlog clear too early", k.tenant, k.event)
		}
	}
	if _, ok := got[names[2]]; ok {
		t.Errorf("stillWaiting reported tenant %s / %s as waiting: its row is stamped, so what the gauge "+
			"holds about that series is the wait of a row that went out, not a backlog", names[2].tenant, names[2].event)
	}
	for _, k := range []lagKey{names[3], names[4]} {
		if _, ok := got[k]; ok {
			t.Errorf("stillWaiting answered a series it was never asked about (tenant %s / %s): the read takes "+
				"the queue's whole pending set to avoid naming the ledger as parameters, and a caller that gets "+
				"back rows of other tenants' series is handed a set it has no business correcting",
				k.tenant, k.event)
		}
	}
}
