package events_test

// The delivery ledger move: what a deployment that starts naming its app owes the
// two tables whose rows are keyed by the durable.
//
// The failure this suite exists for is stated in kit/events/ledger.go, and it is
// narrower than it looks: a scoped consumer's filter still reads the pre-flip
// addresses, but transport.AddressMismatch terminates a message that names no app
// before any handler runs, so a DeliverAll of old traffic is not the bug. The bug
// is a re-publish *after* the flip — the relay carries the same outbox row again,
// this time at the scoped address, and the claim the consumer looks for
// ((event_id, "collect+ledger-invoice-issued")) is not the row in the table
// ((event_id, "ledger-invoice-issued")) — so the handler runs a second time for
// work it already committed. A consumer remake reaches the same place, and a dead
// letter left under the old name leaves the new one free to run a handler that was
// already terminated.
//
// Every case names the mutation it dies on, because the handler count alone proves
// only that a copy happened: the row set is what proves it was a rename.

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

const (
	ledgerModule = "ledger"
	ledgerEvent  = "ledger.invoice_issued"
)

// placeTenant writes the tenants row the delivery boundary reads. The app half is
// the whole point of two of these cases: a tenant an app-less boot stamped is one
// the move sees as nobody's until the deployment names its app.
func placeTenant(t *testing.T, conn *db.Conn, id uuid.UUID, slug, app string) {
	t.Helper()
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		return tx.DB().Exec(`INSERT INTO tenants (id, slug, name, status, app) VALUES (?, ?, ?, 'active', ?)`,
			id, slug, slug, app).Error
	}); err != nil {
		t.Fatalf("place tenant %s in app %q: %v", slug, app, err)
	}
}

// claimRow plants one handled row, and deadRow one terminal row, in the shape the
// kernel writes them. A dead letter without its claim is a real shape — older
// releases wrote it (migrations/000005's header) — and it is the shape the
// dead-letter half of the move is the answer to.
func deadRow(t *testing.T, conn *db.Conn, durable string, eventID, tenantID uuid.UUID) {
	t.Helper()
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		return tx.DB().Exec(`INSERT INTO platformkit_dead_letters (event_id, durable, tenant_id, name, error) VALUES (?, ?, ?, ?, ?)`,
			eventID, durable, tenantID, ledgerEvent, "the mailer was down").Error
	}); err != nil {
		t.Fatalf("plant a dead letter at (%s, %s): %v", eventID, durable, err)
	}
}

func claimRow(t *testing.T, conn *db.Conn, durable string, eventID, tenantID uuid.UUID) {
	t.Helper()
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		return tx.DB().Exec(`INSERT INTO platformkit_handled (event_id, durable, tenant_id) VALUES (?, ?, ?)`,
			eventID, durable, tenantID).Error
	}); err != nil {
		t.Fatalf("plant a platformkit_handled row at (%s, %s): %v", eventID, durable, err)
	}
}

// durables answers which durable rows of the two ledgers sit at, for one event.
func durables(t *testing.T, conn *db.Conn, table string, eventID uuid.UUID) []string {
	t.Helper()
	var out []string
	err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		return tx.DB().Raw(`SELECT durable FROM `+table+` WHERE event_id = ? ORDER BY durable`, eventID).Scan(&out).Error
	})
	if err != nil {
		t.Fatalf("read the %s rows of %s: %v", table, eventID, err)
	}
	return out
}

// movedRecords reads back every platformkit.ledger_moved record of a tenant. This
// is the read of pillar line 3: the record is in the outbox, in the tenant whose
// ledger moved, which is how modules/audit comes to hold it in that tenant's trail
// — the same door EventReplayed goes through, and the same one replay_test.go reads.
func movedRecords(t *testing.T, conn *db.Conn, tenantID uuid.UUID) []events.LedgerMovedRecord {
	t.Helper()
	var bodies []string
	err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		return tx.DB().Raw(`SELECT payload::text FROM platformkit_outbox WHERE name = ? AND tenant_id = ? ORDER BY id`,
			events.EventLedgerMoved, tenantID).Scan(&bodies).Error
	})
	if err != nil {
		t.Fatalf("read the %s records of tenant %s: %v", events.EventLedgerMoved, tenantID, err)
	}
	out := make([]events.LedgerMovedRecord, 0, len(bodies))
	for _, body := range bodies {
		var rec events.LedgerMovedRecord
		if err := json.Unmarshal([]byte(body), &rec); err != nil {
			t.Fatalf("the %s record %q is not the payload it declares: %v", events.EventLedgerMoved, body, err)
		}
		out = append(out, rec)
	}
	return out
}

// unstamp puts one outbox row back on the pending pile — the relay's next pass
// carries it again, which is what Replay does to a row and what a consumer remake
// replays. It is written here rather than called through Replay because Replay
// clears the claims, and the claims are the subject.
func unstamp(t *testing.T, conn *db.Conn, eventID uuid.UUID) {
	t.Helper()
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		return tx.DB().Exec(`UPDATE platformkit_outbox SET published_at = NULL WHERE id = ?`, eventID).Error
	}); err != nil {
		t.Fatalf("un-stamp %s: %v", eventID, err)
	}
}

func eventID(t *testing.T, conn *db.Conn, tenantID uuid.UUID, name string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		return tx.DB().Raw(`SELECT id FROM platformkit_outbox WHERE tenant_id = ? AND name = ?`, tenantID, name).Row().Scan(&id)
	})
	if err != nil {
		t.Fatalf("read the id of %s in tenant %s: %v", name, tenantID, err)
	}
	return id
}

// handler counting is racy by construction: the memory transport hands a delivery
// to its sink on a goroutine of its own.
type counter struct {
	mu    sync.Mutex
	calls int
}

func (c *counter) run() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	return nil
}

func (c *counter) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// TestAMovedLedgerStopsTheSameEventBeingHandledTwice is the brief's own case:
// handle E under the old durable, run the move, republish E through a real
// Consume, and count zero handler calls. It is also the case that says the move is
// a rename: the row set is asserted beside the count, because a copy would leave
// the count at zero too.
func TestAMovedLedgerStopsTheSameEventBeingHandledTwice(t *testing.T) {
	_, conn := dbtest.Schema(t)
	ctx, stop := context.WithCancel(t.Context())
	defer stop()

	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "collect-shop"}
	// The tenant an app-less boot stamped: app '' is why the deployment has a
	// ledger to move at all.
	placeTenant(t, conn, tenant.ID, tenant.Slug, "")

	unscoped := appname.Durable(appname.Name(""), ledgerModule, ledgerEvent)
	scoped := appname.Durable("collect", ledgerModule, ledgerEvent)

	first := new(counter)
	old := memory.New()
	if err := events.Consume(ctx, conn, old, []events.Subscription{{
		Module: ledgerModule, Name: ledgerEvent,
		Handler: func(context.Context, db.Tx[db.Tenant], events.Event) error { return first.run() },
	}}); err != nil {
		t.Fatalf("Consume the unscoped subscription: %v", err)
	}
	publish(t, conn, tenant, ledgerEvent, map[string]string{"why": "the invoice went out"})
	if err := events.Relay(ctx, conn, old); err != nil {
		t.Fatalf("relay: %v", err)
	}
	if first.count() != 1 {
		t.Fatalf("the unscoped subscription handled the event %d times, want once", first.count())
	}
	id := eventID(t, conn, tenant.ID, ledgerEvent)
	if got := durables(t, conn, "platformkit_handled", id); len(got) != 1 || got[0] != unscoped {
		t.Fatalf("the claim sits at %v, want the one unscoped row %q", got, unscoped)
	}

	// The deployment starts naming its app: the tenant is collect's, and the
	// ledger moves.
	if err := dbtest.System(ctx, conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		return tx.DB().Exec(`UPDATE tenants SET app = 'collect' WHERE id = ?`, tenant.ID).Error
	}); err != nil {
		t.Fatalf("name the app that holds the tenant: %v", err)
	}
	report, err := events.MoveLedger(ctx, conn, "collect", "boot")
	if err != nil {
		t.Fatalf("MoveLedger: %v", err)
	}
	if report.Claims != 1 || report.Dead != 0 || report.Subscriptions != 1 || report.Tenants != 1 {
		t.Errorf("the move reported %+v, want 1 claim, 0 dead, 1 subscription, 1 tenant", report)
	}

	// The row set: the claim is the scoped one and no unscoped row is left. This
	// is what dies if the delete is dropped, and what dies if the copy is spelled
	// as a rename of the durable column without the prefix property.
	if got := durables(t, conn, "platformkit_handled", id); len(got) != 1 || got[0] != scoped {
		t.Fatalf("after the move the claim sits at %v, want the one scoped row %q", got, scoped)
	}

	// Republish: the same row, the same id, the relay carrying it again, and a
	// real Consume on the app's own durable.
	second := new(counter)
	fresh := memory.New()
	if err := events.Consume(ctx, conn, fresh, []events.Subscription{{
		App: "collect", Module: ledgerModule, Name: ledgerEvent,
		Handler: func(context.Context, db.Tx[db.Tenant], events.Event) error { return second.run() },
	}}); err != nil {
		t.Fatalf("Consume the scoped subscription: %v", err)
	}
	unstamp(t, conn, id)
	if err := events.RelayApp(ctx, conn, fresh, "collect"); err != nil {
		t.Fatalf("relay as app collect: %v", err)
	}
	if second.count() != 0 {
		t.Fatalf("app collect's handler ran %d times for an event its own durable already claimed; the move did not move the claim", second.count())
	}
	if got := durables(t, conn, "platformkit_handled", id); len(got) != 1 || got[0] != scoped {
		t.Fatalf("the refused delivery changed the ledger: %v", got)
	}
}

// TestTheLedgerMoveMovesEachLedgerOnce covers the idempotence the brief asks for
// and the record that must not be written twice: the second run selects nothing,
// because '+' is appJoin and no scoped durable can satisfy the predicate.
func TestTheLedgerMoveMovesEachLedgerOnce(t *testing.T) {
	_, conn := dbtest.Schema(t)
	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "collect-shop"}
	placeTenant(t, conn, tenant.ID, tenant.Slug, "collect")
	id := uuid.New()
	claimRow(t, conn, appname.Durable(appname.Name(""), ledgerModule, ledgerEvent), id, tenant.ID)

	first, err := events.MoveLedger(t.Context(), conn, "collect", "boot")
	if err != nil {
		t.Fatalf("MoveLedger: %v", err)
	}
	if first.Claims != 1 {
		t.Fatalf("the first move reported %+v, want one claim moved", first)
	}
	second, err := events.MoveLedger(t.Context(), conn, "collect", "boot")
	if err != nil {
		t.Fatalf("the second MoveLedger: %v", err)
	}
	if second != (events.MoveReport{}) {
		t.Errorf("the second move reported %+v, want the zero report: it moved nothing", second)
	}
	if recs := movedRecords(t, conn, tenant.ID); len(recs) != 1 {
		t.Errorf("the tenant's trail holds %d %s records, want the one the first move wrote",
			len(recs), events.EventLedgerMoved)
	}
}

// TestAMovedDeadLetterKeepsItsHandlerFromRunning is the dead-letter half: a
// terminal row under the old name leaves the scoped name free to run a handler the
// transport already gave up on, because claim consults the dead-letter table for
// the durable it is about to write. This dies on the mutation "copy the handled
// ledger only".
func TestAMovedDeadLetterKeepsItsHandlerFromRunning(t *testing.T) {
	_, conn := dbtest.Schema(t)
	ctx, stop := context.WithCancel(t.Context())
	defer stop()

	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "collect-shop"}
	placeTenant(t, conn, tenant.ID, tenant.Slug, "collect")
	unscoped := appname.Durable(appname.Name(""), ledgerModule, ledgerEvent)
	scoped := appname.Durable("collect", ledgerModule, ledgerEvent)
	// The event is outboxed first, so the terminal row can name the id the relay
	// will carry: the dead letter is only worth moving if it is the id a delivery
	// arrives with. The legacy shape — a terminal row and no claim beside it — is
	// what migrations/000005's header says older releases wrote.
	publish(t, conn, tenant, ledgerEvent, nil)
	id := eventID(t, conn, tenant.ID, ledgerEvent)
	deadRow(t, conn, unscoped, id, tenant.ID)

	report, err := events.MoveLedger(ctx, conn, "collect", "boot")
	if err != nil {
		t.Fatalf("MoveLedger: %v", err)
	}
	if report.Dead != 1 || report.Claims != 0 {
		t.Fatalf("the move reported %+v, want one dead letter and no claim", report)
	}
	if got := durables(t, conn, "platformkit_dead_letters", id); len(got) != 1 || got[0] != scoped {
		t.Fatalf("the dead letter sits at %v, want %q", got, scoped)
	}

	// The delivery arrives under the scoped name: the outbox still holds the row,
	// so the relay carries it again.
	ran := new(counter)
	transport := memory.New()
	if err := events.Consume(ctx, conn, transport, []events.Subscription{{
		App: "collect", Module: ledgerModule, Name: ledgerEvent,
		Handler: func(context.Context, db.Tx[db.Tenant], events.Event) error { return ran.run() },
	}}); err != nil {
		t.Fatalf("Consume: %v", err)
	}
	if err := events.RelayApp(ctx, conn, transport, "collect"); err != nil {
		t.Fatalf("relay as app collect: %v", err)
	}
	if ran.count() != 0 {
		t.Fatalf("a handler the transport had terminated ran %d more times under its app's durable", ran.count())
	}
}

// TestAnAppThatNamesNothingMovesNothing is the branch that must not write: an
// app-less deployment has no scoped name to move to, and forming the prefix out of
// an empty slug would write '+mod-ledger…' — somebody else's durable.
func TestAnAppThatNamesNothingMovesNothing(t *testing.T) {
	_, conn := dbtest.Schema(t)
	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "solo-shop"}
	placeTenant(t, conn, tenant.ID, tenant.Slug, "")
	id := uuid.New()
	unscoped := appname.Durable(appname.Name(""), ledgerModule, ledgerEvent)
	claimRow(t, conn, unscoped, id, tenant.ID)
	deadRow(t, conn, unscoped, id, tenant.ID)

	before := ledgerShape(t, conn)
	report, err := events.MoveLedger(t.Context(), conn, appname.Name(""), "boot")
	if err != nil {
		t.Fatalf("MoveLedger for an app-less deployment: %v", err)
	}
	if report != (events.MoveReport{}) {
		t.Errorf("an app-less deployment reported %+v, want the zero report", report)
	}
	if after := ledgerShape(t, conn); after != before {
		t.Errorf("the ledgers changed without an app to move them to:\n before %s\n after  %s", before, after)
	}
	if recs := movedRecords(t, conn, tenant.ID); len(recs) != 0 {
		t.Errorf("an app-less deployment wrote %d records; moving nothing is not an act to record", len(recs))
	}
}

// TestANameThatIsNotASlugMovesNothing is the immutable refusal: a Name built by
// conversion, not Parse, would form the unscoped durable while claiming to be
// scoped, which empties the ledger for the app that does own it.
func TestANameThatIsNotASlugMovesNothing(t *testing.T) {
	_, conn := dbtest.Schema(t)
	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "collect-shop"}
	placeTenant(t, conn, tenant.ID, tenant.Slug, "collect")
	id := uuid.New()
	claimRow(t, conn, appname.Durable(appname.Name(""), ledgerModule, ledgerEvent), id, tenant.ID)

	before := ledgerShape(t, conn)
	_, err := events.MoveLedger(t.Context(), conn, appname.Name("Collect EU"), "boot")
	if err == nil {
		t.Fatal("a name a subject token cannot hold moved the ledger; it has no durable of its own to move to")
	}
	if !strings.Contains(err.Error(), "not an app name") {
		t.Errorf("the refusal was %q, want the sentence that says what to fix", err)
	}
	if after := ledgerShape(t, conn); after != before {
		t.Errorf("a refused move changed the ledgers:\n before %s\n after  %s", before, after)
	}
	if recs := movedRecords(t, conn, tenant.ID); len(recs) != 0 {
		t.Errorf("a refused move wrote %d records; a refusal emits nothing", len(recs))
	}
}

// TestTheFirstAppToMoveOwnsTheLedger is two apps on one database: the unscoped
// history names no app, so the app that moves first takes it and the second finds
// nothing to take. A copy would report the same rows for academy, and academy
// would then skip events it never handled — the case the spec's §1.7 names.
func TestTheFirstAppToMoveOwnsTheLedger(t *testing.T) {
	_, conn := dbtest.Schema(t)
	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "collect-shop"}
	placeTenant(t, conn, tenant.ID, tenant.Slug, "collect")
	other := tenantID(t, conn, "academy-school", "academy")
	id := uuid.New()
	claimRow(t, conn, appname.Durable(appname.Name(""), ledgerModule, ledgerEvent), id, tenant.ID)
	claimRow(t, conn, appname.Durable(appname.Name(""), ledgerModule, ledgerEvent), uuid.New(), other)

	if _, err := events.MoveLedger(t.Context(), conn, "collect", "boot"); err != nil {
		t.Fatalf("MoveLedger as collect: %v", err)
	}
	// The tenant's own record from the first move is in that tenant's trail: the
	// unscoped rows named no app, and the app that moved first owns that history.
	// What academy may not do is add a record of its own for a move that moved
	// nothing.
	before := len(movedRecords(t, conn, other))
	moved, err := events.MoveLedger(t.Context(), conn, "academy", "boot")
	if err != nil {
		t.Fatalf("MoveLedger as academy: %v", err)
	}
	if moved != (events.MoveReport{}) {
		t.Errorf("app academy reported %+v; every unscoped row is gone, so it moves nothing and says so", moved)
	}
	if after := len(movedRecords(t, conn, other)); after != before {
		t.Errorf("app academy emitted %d more records for a ledger it did not move", after-before)
	}
	if n := claimsUnder(t, conn, appname.Durable("collect", ledgerModule, ledgerEvent)); n != 2 {
		t.Errorf("%d claims sit under collect's durable, want the two unscoped rows it moved", n)
	}
	if n := claimsUnder(t, conn, appname.Durable("academy", ledgerModule, ledgerEvent)); n != 0 {
		t.Errorf("%d claims sit under academy's durable, want none: a copy would have given academy work it never did", n)
	}
}

// TestAMovedLedgerIsRecordedInTheTenantWhoseClaimMoved is pillar line 3's read
// back: the act is an event, in the tenant whose rows moved, naming the app, the
// durables, both counts and who asked — and the boot path names no actor, because
// nothing did.
func TestAMovedLedgerIsRecordedInTheTenantWhoseClaimMoved(t *testing.T) {
	_, conn := dbtest.Schema(t)
	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "collect-shop"}
	placeTenant(t, conn, tenant.ID, tenant.Slug, "collect")
	other := tenantID(t, conn, "other-shop", "collect")
	deadRow(t, conn, appname.Durable(appname.Name(""), "billing", "billing.plan.created"), uuid.New(), other)
	claimRow(t, conn, appname.Durable(appname.Name(""), ledgerModule, ledgerEvent), uuid.New(), other)

	if _, err := events.MoveLedger(t.Context(), conn, "collect", "boot"); err != nil {
		t.Fatalf("MoveLedger: %v", err)
	}
	if recs := movedRecords(t, conn, tenant.ID); len(recs) != 0 {
		t.Errorf("tenant %s got %d records for a ledger with no rows at all; the write that finds none is not written", tenant.ID, len(recs))
	}
	otherRecs := movedRecords(t, conn, other)
	if len(otherRecs) != 1 || otherRecs[0].Claims != 1 || otherRecs[0].Dead != 1 {
		t.Fatalf("the tenant whose rows moved got %v, want one record of 1 claim and 1 dead letter", otherRecs)
	}
	for _, want := range []string{
		appname.Durable("collect", "billing", "billing.plan.created"),
		appname.Durable("collect", ledgerModule, ledgerEvent),
	} {
		if !slices.Contains(otherRecs[0].Durables, want) {
			t.Errorf("the record names durables %v, want %q among them", otherRecs[0].Durables, want)
		}
	}
	if got := appname.Durable(appname.Name(""), "billing", "billing.plan.created"); slices.Contains(otherRecs[0].Durables, got) {
		t.Errorf("the record names the unscoped durable %q; it says where the rows went, not where they were", got)
	}
	if otherRecs[0].RequestedBy != "boot" {
		t.Errorf("the record says %q asked; the worker's own step says boot", otherRecs[0].RequestedBy)
	}
	// The actor is the envelope's answer to "who did this", read off the context
	// by the one INSERT every event passes through. A boot has nobody.
	var actors []uuid.UUID
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		return tx.DB().Raw(`SELECT coalesce(actor, '00000000-0000-0000-0000-000000000000') FROM platformkit_outbox WHERE name = ?`,
			events.EventLedgerMoved).Scan(&actors).Error
	}); err != nil {
		t.Fatalf("read the actors of the move records: %v", err)
	}
	if len(actors) != 1 {
		t.Fatalf("%d move records, want the one tenant whose rows moved", len(actors))
	}
	for _, a := range actors {
		if a != uuid.Nil {
			t.Errorf("a boot move recorded actor %s; nobody did it", a)
		}
	}
}

// TestADeliveryMidClaimRefusesTheMove is the concurrency-honesty case: the move
// refuses rather than waiting, and refuses without writing, so the retry — boot,
// the kernel job, an operator — is what finishes it. It fails as a hang without
// NOWAIT and as a lost claim without the lock at all.
func TestADeliveryMidClaimRefusesTheMove(t *testing.T) {
	_, conn := dbtest.Schema(t)
	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "collect-shop"}
	placeTenant(t, conn, tenant.ID, tenant.Slug, "collect")
	id := uuid.New()
	unscoped := appname.Durable(appname.Name(""), ledgerModule, ledgerEvent)
	claimRow(t, conn, unscoped, id, tenant.ID)

	held, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		// A delivery's own transaction, holding the claim's insert lock to its end,
		// which is what claim does while its handler runs.
		err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
			if err := tx.DB().Exec(`INSERT INTO platformkit_handled (event_id, durable, tenant_id) VALUES (?, ?, ?)`,
				uuid.New(), unscoped, tenant.ID).Error; err != nil {
				return err
			}
			close(held)
			<-release
			return nil
		})
		if err != nil {
			t.Errorf("the transaction holding a claim: %v", err)
		}
	}()
	<-held

	_, err := events.MoveLedger(t.Context(), conn, "collect", "boot")
	close(release)
	<-done
	if err == nil {
		t.Fatal("the move took the table lock over a delivery mid-claim; it refuses instead")
	}
	if !strings.Contains(err.Error(), "no ledger row moved, run it again") {
		t.Errorf("the refusal was %q, want the sentence that says nothing moved and to run it again", err)
	}
	if recs := movedRecords(t, conn, tenant.ID); len(recs) != 0 {
		t.Errorf("the refused move wrote %d records; a refusal emits nothing", len(recs))
	}
	// The delivery committed behind the refusal: its own claim is there, and the
	// row the move refused to take is where it was.
	if n := claimsUnder(t, conn, unscoped); n != 2 {
		t.Errorf("%d claims sit under the unscoped durable %q after the refusal, want the planted row and the delivery's own", n, unscoped)
	}
	if n := claimsUnder(t, conn, appname.Durable("collect", ledgerModule, ledgerEvent)); n != 0 {
		t.Errorf("%d claims sit under the scoped durable after a move that refused, want none: it writes nothing", n)
	}
}

// TestAMovedClaimIsInvisibleToTheOtherTenant is pillar line 1: the copy carries
// tenant_id verbatim, so FORCE ROW LEVEL SECURITY answers to the moved row exactly
// as it answered to the row it replaced. It fails the day the copy writes the
// mover's tenant instead of the row's.
func TestAMovedClaimIsVisibleOnlyToItsOwnTenant(t *testing.T) {
	_, conn := dbtest.Schema(t)
	collect := tenantID(t, conn, "collect-shop", "collect")
	other := tenantID(t, conn, "other-shop", "collect")
	unscoped := appname.Durable(appname.Name(""), ledgerModule, ledgerEvent)
	mine, theirs := uuid.New(), uuid.New()
	claimRow(t, conn, unscoped, mine, collect)
	claimRow(t, conn, unscoped, theirs, other)
	scoped := appname.Durable("collect", ledgerModule, ledgerEvent)

	if _, err := events.MoveLedger(t.Context(), conn, "collect", "boot"); err != nil {
		t.Fatalf("MoveLedger: %v", err)
	}
	for _, c := range []struct {
		who  uuid.UUID
		want int
	}{{collect, 1}, {other, 1}} {
		var n int
		err := db.Run(tenancy.WithTenant(t.Context(), tenancy.Tenant{ID: c.who}), conn,
			func(_ context.Context, tx db.Tx[db.Tenant]) error {
				return tx.DB().Raw(`SELECT count(*) FROM platformkit_handled WHERE durable = ?`, scoped).Row().Scan(&n)
			})
		if err != nil {
			t.Fatalf("count %s's moved claims as tenant %s: %v", scoped, c.who, err)
		}
		if n != c.want {
			t.Errorf("tenant %s sees %d claims under %s, want %d: the ledger is tenant-owned after the move as it was before", c.who, n, scoped, c.want)
		}
	}
	// And a tenant nobody moved keeps its own count rather than the sum.
	var total int
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		return tx.DB().Raw(`SELECT count(*) FROM platformkit_handled WHERE durable = ?`, scoped).Row().Scan(&total)
	}); err != nil {
		t.Fatalf("count the moved claims: %v", err)
	}
	if total != 2 {
		t.Errorf("%d claims sit under %s across tenants, want the two this move renamed", total, scoped)
	}
}

// tenantID places a tenant and answers its id.
func tenantID(t *testing.T, conn *db.Conn, slug, app string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	placeTenant(t, conn, id, slug, app)
	return id
}

// ledgerShape is the whole content of both ledgers, for the assertions that say
// nothing changed. It names durable, tenant and event, which is every column
// except the two timestamps the move copies rather than re-stamps.
func ledgerShape(t *testing.T, conn *db.Conn) string {
	t.Helper()
	var out []string
	err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		return tx.DB().Raw(`SELECT 'handled '||durable||' '||tenant_id||' '||event_id FROM platformkit_handled
				UNION ALL SELECT 'dead '||durable||' '||tenant_id||' '||event_id FROM platformkit_dead_letters
				ORDER BY 1`).Scan(&out).Error
	})
	if err != nil {
		t.Fatalf("read the shape of the ledgers: %v", err)
	}
	return strings.Join(out, "\n")
}

func claimsUnder(t *testing.T, conn *db.Conn, durable string) int {
	t.Helper()
	var n int
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		return tx.DB().Raw(`SELECT count(*) FROM platformkit_handled WHERE durable = ?`, durable).Row().Scan(&n)
	}); err != nil {
		t.Fatalf("count the claims under %s: %v", durable, err)
	}
	return n
}
