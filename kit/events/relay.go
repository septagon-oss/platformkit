package events

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events/internal/delivery"
	"github.com/septagon-oss/platformkit/kit/internal/syscap"
	"github.com/septagon-oss/platformkit/kit/telemetry"
)

// batch bounds one relay pass. Small enough that a pass is short and its rows
// are locked briefly, large enough that a burst drains in a few ticks.
const batch = 100

// The relay and the purge read every tenant's rows, which is the whole reason
// the capability exists; both reasons are logged wherever a system transaction
// opens.
var (
	relayToken = syscap.NewSystemToken("outbox relay")
	purgeToken = syscap.NewSystemToken("outbox purge")
)

// row is one outbox record as the relay reads it. Actor is a pointer because
// the column is null for everything nobody asked for.
type row struct {
	ID        uuid.UUID
	TenantID  uuid.UUID
	Name      string
	Payload   []byte
	CreatedAt time.Time
	Actor     *uuid.UUID
	// The publisher's trace context, its correlation member, and the call the
	// event was written by. Absent is NULL — see nilIfEmpty and the rows this file
	// shipped before either column existed — and the relay reads all five as the
	// empty string, because its own contract is the envelope's optional members
	// and the trail's empty string, not the column's nullability. It reads each
	// row's trace into that row's own publication span and onto the envelope, not
	// into the pass span: one batch is many unrelated traces, and the pass is the
	// worker's. See startPublication and trace.go.
	//
	// The column tags are load-bearing. GORM maps a scanned field by snake_case,
	// so TraceParent arrives as trace_parent: without the tag, naming the SELECT
	// alias after the column is how a carried fact silently stops being carried.
	// The tag binds field to column explicitly, which is why the aliases below can
	// be the COALESCE that turns a NULL into the empty string this struct holds.
	TraceParent string `gorm:"column:traceparent"`
	TraceState  string `gorm:"column:tracestate"`
	Baggage     string `gorm:"column:baggage"`
	RequestID   string `gorm:"column:request_id"`
	ClientIP    string `gorm:"column:client_ip"`
}

// lagKey is one pkit.outbox.lag time series: one tenant's share of one event name.
type lagKey struct {
	tenant uuid.UUID
	event  string
}

// oldestPerSeries calls record once for each (tenant, event) series in a batch, on
// the first row of it. The gauge is last-write-wins per series, so which row a pass
// records is which wait the number ends up showing — and the batch is read
// ORDER BY created_at, so the first row of a series is the oldest row the queue was
// holding. Recording every row instead leaves the series holding the newest row's
// wait, and the reading that answers "is this queue draining?" becomes the shortest
// wait in the batch: ten seconds, about a backlog whose oldest row is five minutes
// old. Which row answers is the whole content of the number, which is why the choice
// is a function of its own rather than a line inside the relay loop.
func oldestPerSeries(rows []row, record func(row)) {
	seen := make(map[lagKey]struct{}, len(rows))
	for _, r := range rows {
		k := lagKey{tenant: r.TenantID, event: r.Name}
		if _, again := seen[k]; again {
			continue
		}
		seen[k] = struct{}{}
		record(r)
	}
}

// lagLedger remembers which (tenant, event) series this process last reported a wait
// for, so a pass that finds no row of them can say what the queue says now.
//
// The instrument is a gauge, and a gauge keeps the value it was last given. A series
// whose last row went out therefore keeps reporting that row's wait — "this tenant's
// billing.invoice_issued is 300 seconds behind" about an event nobody has published
// since the backlog drained — until the same tenant publishes that same name again, which
// for an event published once a day is tomorrow. The relay is the thing that can say
// otherwise: it runs every second in the worker role, so a series that has drained is
// noticed within a second of draining and gets a zero. It tracks only this process's own
// readings, which is the right scope: the gauge lives in this process's SDK, and a
// restart starts its collections from nothing anyway.
type lagLedger struct {
	mu   sync.Mutex
	seen map[lagKey]struct{}
}

// reportedSeries is this process's ledger. Relay is exported and a process may relay
// from more than one goroutine, so it is locked rather than assumed single-threaded.
var reportedSeries = new(lagLedger)

// remember notes that a pass just reported this series, so a later pass owes it a zero.
func (l *lagLedger) remember(k lagKey) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.seen == nil {
		l.seen = map[lagKey]struct{}{}
	}
	l.seen[k] = struct{}{}
}

// drainedSeries answers the whole correction — which series a pass owes a zero to — as a
// choice over three sets, for the same reason oldestPerSeries is a function of its own:
// the choice is the content of the number.
//
// reported is what earlier passes recorded a wait for, held is what this pass has rows of,
// and waiting is the part of reported that still has an unpublished row in the queue. A
// series this pass holds is not drained: its value comes from its own row a few lines
// later, and writing both would leave the reading to whichever write landed last. A series
// the queue still holds a row of is not drained either — including a row another worker
// took under FOR UPDATE SKIP LOCKED, whose wait is still real — so the correction errs
// towards repeating one stale reading rather than announcing a backlog clear while rows of
// it are waiting.
func drainedSeries(reported, held, waiting map[lagKey]struct{}) []lagKey {
	var drained []lagKey
	for k := range reported {
		if _, inBatch := held[k]; inBatch {
			continue
		}
		if _, pending := waiting[k]; pending {
			continue
		}
		drained = append(drained, k)
	}
	return drained
}

// settleDrained writes the zero drainedSeries asks for and drops those series from the
// ledger, so a queue that has been idle for a pass costs nothing further: a corrected
// series is not asked about again until a row of it is relayed. The read underneath it
// names no series as parameters, so a ledger grown by a backlog across a hundred tenants
// costs one read of the queue, and cannot refuse the pass that owes the correction. It is
// called from the pass that finds the queue's end and from no other — see relayBatch.
func (l *lagLedger) settleDrained(ctx context.Context, tx db.Tx[db.System], held map[lagKey]struct{}) error {
	l.mu.Lock()
	reported := make(map[lagKey]struct{}, len(l.seen))
	for k := range l.seen {
		if _, inBatch := held[k]; !inBatch {
			reported[k] = struct{}{}
		}
	}
	l.mu.Unlock()
	if len(reported) == 0 {
		return nil
	}
	waiting, err := stillWaiting(ctx, tx, reported)
	if err != nil {
		return err
	}
	for _, k := range drainedSeries(reported, held, waiting) {
		telemetry.Shared().ObserveOutboxLag(ctx, 0,
			attribute.String(telemetry.AttrTenantID, k.tenant.String()),
			attribute.String("pkit.event", k.event))
		l.mu.Lock()
		delete(l.seen, k)
		l.mu.Unlock()
	}
	return nil
}

// stillWaiting answers which of these series the queue still holds an unpublished row of.
//
// The question goes to the queue carrying no bind parameters at all: one `SELECT DISTINCT
// tenant_id, name` over the unpublished rows, filtered against reported on the way back.
// What it used to do — name every reported series as two parameters of one statement — put
// the correction's cost, and whether it happens at all, in proportion to how many readings
// this process has taken rather than to how much work the queue holds. A Postgres statement
// carries at most 65,535 parameters, and this repository's own server refuses a long list of
// row constructors with `stack depth limit exceeded` somewhere between 6,000 and 8,000
// series, well before that ceiling; the read runs inside the relay's transaction, so the
// refusal took the batch down with it — nothing published, nothing stamped, the ledger
// unshortened by a pass that failed, and the next tick asking the same question and being
// refused again for every tenant until the process restarted. That is a measurement becoming
// the reason an application stops answering, which is what kit/app/telemetry.go exists to
// refuse.
//
// The read's size is the queue's own: the distinct series with an unpublished row. It is
// asked on the pass that finds the queue's end, which is the pass whose answer is worth
// having and the pass with the fewest rows to read about — Relay keeps taking batches until
// one comes back short, and the rows a concurrent worker holds are locked for one batch
// apiece, so a short batch means a short pending set. A LIMIT here would cap the row count
// by making the answer a lie, because a truncated DISTINCT cannot prove any one series
// drained, and the correction's one safe bias is to repeat a stale reading rather than
// announce a clear. Rows of series nobody asked about are dropped from the answer rather
// than returned, so the caller sees the set it named a question about, and no answer costs
// more than the queue it reads at the moment it is read.
func stillWaiting(ctx context.Context, tx db.Tx[db.System], reported map[lagKey]struct{}) (map[lagKey]struct{}, error) {
	var rows []struct {
		TenantID uuid.UUID `gorm:"column:tenant_id"`
		Name     string    `gorm:"column:name"`
	}
	const q = `SELECT DISTINCT tenant_id, name FROM ` + table + ` WHERE published_at IS NULL`
	if err := tx.DB().WithContext(ctx).Raw(q).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("events: relay: read the series that are still waiting: %w", err)
	}
	out := make(map[lagKey]struct{}, len(rows))
	for _, r := range rows {
		k := lagKey{tenant: r.TenantID, event: r.Name}
		if _, asked := reported[k]; asked {
			out[k] = struct{}{}
		}
	}
	return out, nil
}

// Relay moves every unpublished row to the transport, a batch at a time, and
// returns when the queue is empty or ctx is done. kit/jobs calls it every second
// in the worker role.
//
// It takes no lock, and that is deliberate: FOR UPDATE SKIP LOCKED is the
// concurrency control, so several workers relaying at once each take rows
// nobody else holds and none of them waits. An advisory lock around it would
// add nothing and would take something away — a transport that blocks inside
// one relay pass would hold that lock, and every other periodic job on every
// replica would stop behind it.
//
// One transaction per batch rather than one for the whole drain, so the row
// locks are held for a batch and not for a backlog. The caller bounds the whole
// thing with a deadline; a pass that runs out of time leaves its rows unstamped
// and the next tick takes them.
func Relay(ctx context.Context, conn *db.Conn, t Transport) error {
	for {
		n, err := relayBatch(ctx, conn, t)
		if err != nil || n < batch {
			return err
		}
	}
}

// relayBatch moves at most batch rows and reports how many it moved.
//
// The publish happens before the stamp, so a crash in between redelivers rather
// than loses — see the package comment on idempotency.
func relayBatch(ctx context.Context, conn *db.Conn, t Transport) (int, error) {
	// One span for the pass, and one per row below it. What a reader wants from a
	// pass span is whether the queue is draining and how long a pass took — one
	// fact about a hundred rows, and one the worker's own trace should hold. Whose
	// rows went out is the other fact, and a batch is read across every tenant, so
	// no tenant this span could name would name the rest: that half belongs on the
	// publication span of each row (startPublication), which hangs off that row's
	// own trace and so leaves this trace the single span it is here. A pass that
	// moved nothing still makes one, because "the relay runs and moves nothing" is
	// the answer to a question somebody asked.
	ctx, span := telemetry.Tracer().Start(ctx, "outbox relay batch")
	defer func() { span.End() }()
	var moved int
	err := db.RunSystem(ctx, conn, relayToken, func(ctx context.Context, tx db.Tx[db.System]) error {
		var rows []row
		// The aliases name the columns, and the row struct's tags name the fields, so
		// GORM's snake_case mapping never gets a chance to miss them; the COALESCE is
		// what turns a column that is NULL — the ordinary case, see nilIfEmpty — into
		// the empty string the envelope's optional members are written for.
		const q = `SELECT id, tenant_id, name, payload, created_at, actor,
			COALESCE(traceparent, '') AS traceparent, COALESCE(tracestate, '') AS tracestate,
			COALESCE(baggage, '') AS baggage, COALESCE(request_id, '') AS request_id,
			COALESCE(host(client_ip), '') AS client_ip FROM ` + table + `
			WHERE published_at IS NULL ORDER BY created_at, id LIMIT ? FOR UPDATE SKIP LOCKED`
		if err := tx.DB().Raw(q, batch).Scan(&rows).Error; err != nil {
			return fmt.Errorf("events: relay: read the outbox: %w", err)
		}
		held := make(map[lagKey]struct{}, len(rows))
		for _, r := range rows {
			held[lagKey{tenant: r.TenantID, event: r.Name}] = struct{}{}
		}
		// Only the pass that finds the queue's end asks, and only before anything this
		// pass records: a pass that moves the last row of a series leaves that series with
		// no row to speak for it, and the zero it is owed is what keeps the gauge's number
		// about the queue as it is now. The question goes to the queue's whole pending set,
		// so a pass holding a full batch with the end of the queue nowhere in sight would
		// scan a backlog it cannot yet draw a conclusion about — and Relay takes those
		// batches one after another, so one drain of N rows would pay for the same scan N
		// over batch times over rows it is emptying as it goes. A full batch is itself the
		// reason to wait: it says the queue is not at its end, so which of this process's
		// earlier readings are stale is not knowable yet. The correction therefore lands at
		// the end of the drain that drained the series, inside the same tick, and the state
		// that repeats a stale reading is a tick whose deadline passes mid-drain — the
		// bias this file declares, a number a moment too old rather than a backlog called
		// clear while its rows wait.
		if len(rows) < batch {
			if err := reportedSeries.settleDrained(ctx, tx, held); err != nil {
				return err
			}
		}
		if len(rows) == 0 {
			return nil
		}
		// The number the queue is measured by: how long a row waited between its
		// commit and the relay that took it, per event and per tenant, because a lag
		// that cannot be attributed to a tenant cannot tell you whose events are
		// stuck. Recorded before the batch goes out, once per series, on the row
		// oldestPerSeries picks.
		oldestPerSeries(rows, func(r row) {
			telemetry.Shared().ObserveOutboxLag(ctx, db.Now().Sub(r.CreatedAt).Seconds(),
				attribute.String(telemetry.AttrTenantID, r.TenantID.String()),
				attribute.String("pkit.event", r.Name))
			reportedSeries.remember(lagKey{tenant: r.TenantID, event: r.Name})
		})
		ids := make([]uuid.UUID, 0, len(rows))
		for _, r := range rows {
			ev := Event{ID: r.ID, Name: r.Name, TenantID: r.TenantID, Payload: r.Payload, At: r.CreatedAt,
				TraceParent: r.TraceParent, TraceState: r.TraceState, Baggage: r.Baggage,
				RequestID: r.RequestID, ClientIP: r.ClientIP}
			if r.Actor != nil {
				ev.Actor = *r.Actor
			}
			// One span for this row's departure, on this row's own trace, naming
			// this row's tenant. It carries the row's trace context rather than the
			// pass's, and it is the context the transport is handed, so anything the
			// adapter opens underneath is on the trace of the request that caused the
			// event. See startPublication.
			pubCtx, publication := startPublication(ctx, ev)
			err := t.Publish(pubCtx, ev)
			endSpan(publication, err)
			if err != nil {
				// The rows published so far are still unstamped, so they go
				// again next tick. That is the at-least-once bargain.
				return fmt.Errorf("events: relay: publish %s: %w", r.Name, err)
			}
			ids = append(ids, r.ID)
		}
		if err := tx.DB().Exec("UPDATE "+table+" SET published_at = clock_timestamp() WHERE id IN ?", ids).Error; err != nil {
			return fmt.Errorf("events: relay: stamp %d row(s): %w", len(ids), err)
		}
		moved = len(ids)
		return nil
	})
	span.SetAttributes(attribute.Int("pkit.events.relayed", moved))
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	return moved, err
}

// Purge removes published outbox history after a week. Unpublished rows remain
// recoverable regardless of age. An old handling claim remains while either its
// outbox row or terminal failure record exists: losing that claim would replay
// completed work when the pending row is relayed. Dead letters and their claims
// require explicit operator review; they are never automatically purged.
//
// A dead letter keeps its outbox row with it. The row is the only place the
// payload survives — the dead letter records the failure, not the body — so a
// purge that took the row would leave an operator reviewing a terminal failure
// whose payload is gone and whose replay answers that there is nothing to
// replay. That is the write that takes the last copy away, and a dead letter is
// still referring to the row when it happens, so the row stays. The cost is
// bounded and worth naming: an unread dead letter keeps one JSONB row alive
// past the window, and clearing it is the operator's verb — `events.Replay` or
// an operator who decides the event will never run again deletes the dead
// letter, after which the next purge takes the row.
//
// The database clock supplies the cutoff for all workers.
func Purge(ctx context.Context, conn *db.Conn) error {
	return db.RunSystem(ctx, conn, purgeToken, func(ctx context.Context, tx db.Tx[db.System]) error {
		age := fmt.Sprintf("%d seconds", int(delivery.Keep.Seconds()))
		if err := tx.DB().Exec("DELETE FROM "+table+" o WHERE published_at IS NOT NULL AND published_at < now() - ?::interval"+
			" AND NOT EXISTS (SELECT 1 FROM "+deadLetters+" d WHERE d.event_id = o.id)", age).Error; err != nil {
			return fmt.Errorf("events: purge: %w", err)
		}
		if err := tx.DB().Exec("DELETE FROM "+handled+" h WHERE handled_at < now() - ?::interval"+
			" AND NOT EXISTS (SELECT 1 FROM "+table+" o WHERE o.id = h.event_id)"+
			" AND NOT EXISTS (SELECT 1 FROM "+deadLetters+" d WHERE d.event_id = h.event_id AND d.durable = h.durable)", age).Error; err != nil {
			return fmt.Errorf("events: purge the handled marks: %w", err)
		}
		return nil
	})
}
