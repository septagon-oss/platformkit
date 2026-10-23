package audittest

import (
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/modules/audit/contracts"
)

func recordEvent(t *testing.T, f Fixture) {
	ev := event("task.task.created", ada, noon)
	f.silent(t, "Record", func() {
		if err := f.Service.Record(f.Ctx, f.Tx, ev); err != nil {
			t.Fatalf("Record: %v", err)
		}
	})
	rows, total, err := f.Service.List(f.Ctx, f.Tx, contracts.Query{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 1 || len(rows) != 1 {
		t.Fatalf("the trail holds %d rows, want one", total)
	}
	got := rows[0]
	switch {
	case got.Name != ev.Name:
		t.Errorf("name is %q, want %q", got.Name, ev.Name)
	case got.EventID != ev.ID:
		t.Errorf("eventId is %s, want the event's own id %s", got.EventID, ev.ID)
	case got.Actor == nil || *got.Actor != ada:
		t.Errorf("actor is %v, want %s", got.Actor, ada)
	case !got.OccurredAt.Equal(noon):
		t.Errorf("occurredAt is %s, want when the state changed, %s", got.OccurredAt, noon)
	case compact(t, got.Payload) != compact(t, ev.Payload):
		t.Errorf("payload is %s, want what the module published: %s", got.Payload, ev.Payload)
	}
	back, err := f.Service.Get(f.Ctx, f.Tx, got.ID)
	if err != nil || back.EventID != ev.ID {
		t.Errorf("Get = %v, %v; want the row List returned", back, err)
	}
}

func recordUnattributedWork(t *testing.T, f Fixture) {
	if err := f.Service.Record(f.Ctx, f.Tx, event("task.sla_breached", uuid.Nil, noon)); err != nil {
		t.Fatalf("Record: %v", err)
	}
	rows, _, err := f.Service.List(f.Ctx, f.Tx, contracts.Query{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 1 || rows[0].Actor != nil {
		t.Errorf("a job's event is credited to %v; nobody asked for it", rows[0].Actor)
	}
}

func recordDuplicateEvent(t *testing.T, f Fixture) {
	ev := event("user.invited", ada, noon)
	for range 3 {
		if err := f.Service.Record(f.Ctx, f.Tx, ev); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}
	// The kernel claims each delivery, so this is the second lock, for
	// what the first cannot cover: a replayed outbox row, or a handler
	// that failed after writing.
	if _, total, err := f.Service.List(f.Ctx, f.Tx, contracts.Query{}); err != nil || total != 1 {
		t.Errorf("three deliveries of one event left %d rows (%v), want one", total, err)
	}
}

func recordTransactionTenant(t *testing.T, f Fixture) {
	// The envelope names a tenant, and Consume opened this transaction
	// in it, so the two always agree in production. Taking the tenant
	// from the transaction anyway is what every other write here does,
	// and it is the difference between an isolation the database
	// enforces and one a struct field asks for.
	if err := f.Service.Record(f.Ctx, f.Tx, event("tenant.created", ada, noon)); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if _, total, err := f.Service.List(f.Ctx, f.Tx, contracts.Query{}); err != nil || total != 1 {
		t.Errorf("the row landed somewhere else: %d rows, %v", total, err)
	}
}
