package events_test

import (
	"context"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

func TestScopedDeliveryHonorsAnUnmovedTerminalRecord(t *testing.T) {
	_, conn := dbtest.Schema(t)
	tenant := tenantID(t, conn, "late", "")
	id := uuid.New()
	old := appname.Durable("", ledgerModule, ledgerEvent)
	// Older releases can leave a terminal record without a handled claim.
	deadRow(t, conn, old, id, tenant)
	transport := memory.New()
	var calls atomic.Int64
	if err := events.Consume(t.Context(), conn, transport, []events.Subscription{{
		App: "collect", Module: ledgerModule, Name: ledgerEvent,
		Handler: func(context.Context, db.Tx[db.Tenant], events.Event) error {
			calls.Add(1)
			return nil
		},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := dbtest.System(t.Context(), conn, func(_ context.Context, tx db.Tx[db.System]) error {
		return tx.DB().Exec(`UPDATE tenants SET app = 'collect' WHERE id = ?`, tenant).Error
	}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := transport.Publish(t.Context(), events.Event{ID: id, TenantID: tenant, App: "collect", Name: ledgerEvent}); err != nil {
			t.Fatal(err)
		}
	}
	if got := calls.Load(); got != 0 {
		t.Errorf("terminated event ran %d times before the ledger move", got)
	}
	if got := durables(t, conn, "platformkit_handled", id); len(got) != 0 {
		t.Errorf("terminal refusal wrote claims: %v", got)
	}
	if got := durables(t, conn, "platformkit_dead_letters", id); !slices.Equal(got, []string{old}) {
		t.Errorf("terminal refusal changed the old ledger: %v", got)
	}
	// The same subscription must still accept work that has no terminal record.
	fresh := uuid.New()
	if err := transport.Publish(t.Context(), events.Event{ID: fresh, TenantID: tenant, App: "collect", Name: ledgerEvent}); err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("fresh event left handler count at %d, want 1", got)
	}
	if got := durables(t, conn, "platformkit_handled", fresh); !slices.Equal(got, []string{appname.Durable("collect", ledgerModule, ledgerEvent)}) {
		t.Errorf("fresh event's scoped claim: %v", got)
	}
}
