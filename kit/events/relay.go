package events

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/request"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/septagon-oss/platformkit/kit/appname"
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
	// The attribution the publisher stored beside the actor, pointers for the same
	// reason Actor is: an event with no attribution is a person's request, and the
	// envelope says so by leaving the attributes out.
	ActorKind         *string
	SourceFile        *string
	SourceLine        *int
	Initiator         *uuid.UUID
	OperationID       *string
	OperationRef      *uuid.UUID
	OperationTenantID *uuid.UUID
	OperationPersonID *uuid.UUID
	OperationOutcome  *request.Outcome
	OperationRevision *int64
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

// stillWaiting returns the reported series that still have unpublished rows.
// One unbounded DISTINCT scan avoids parameter limits and cannot falsely declare
// a truncated series drained; its cost follows the pending queue, not telemetry
// history. It runs only when a relay batch reaches the queue's end.
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

// Relay publishes the rows of the deployment that names no app in batches until a batch
// is short or ctx ends.
//
// Deprecated: use RelayFor with the app this process relays for, or
// appname.Name("") for the deployment that names no app.
func Relay(ctx context.Context, conn *db.Conn, t Transport) error {
	return RelayFor(ctx, conn, t, "")
}

// RelayApp relays one app's rows.
//
// Deprecated: use RelayFor, which is the same claim stated once and named for the app
// whose rows it moves.
func RelayApp(ctx context.Context, conn *db.Conn, t Transport, app appname.Name) error {
	return RelayFor(ctx, conn, t, app)
}

// RelayFor relays only the rows whose tenant belongs to app, using one transaction per
// batch, until a batch is short or ctx ends. FOR UPDATE SKIP LOCKED lets workers claim
// independent batches, so two relays of one app move different rows and locks last for
// one batch. Publication precedes stamping, so a crash between the two leaves the row
// retryable and may deliver a duplicate.
//
// Which rows are the app's is the table's answer (tenants.app), read when claiming,
// before anything is published or stamped: an unnamed deployment also owns rows whose
// tenant is gone, and a named app owns nothing that is not its own. The appname.Name("")
// of the deployment that names no app is the whole of the unscoped relay it always ran.
func RelayFor(ctx context.Context, conn *db.Conn, t Transport, app appname.Name) error {
	for {
		n, err := relayBatch(ctx, conn, t, app)
		if err != nil || n < batch {
			return err
		}
	}
}

// relayBatch moves at most batch rows and reports how many it moved.
//
// The publish happens before the stamp, so a crash in between redelivers rather
// than loses — see the package comment on idempotency.
func relayBatch(ctx context.Context, conn *db.Conn, t Transport, app appname.Name) (int, error) {
	// The batch span measures the cross-tenant pass; publication spans retain
	// each row's own tenant and originating trace.
	ctx, span := telemetry.Tracer().Start(ctx, "outbox relay batch")
	defer func() { span.End() }()
	var moved int
	err := db.RunSystem(ctx, conn, relayToken, func(ctx context.Context, tx db.Tx[db.System]) error {
		rows, err := relayRows(tx, app)
		if err != nil {
			return err
		}
		if err := observeRelayLag(ctx, tx, rows); err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		ids := make([]uuid.UUID, 0, len(rows))
		for _, r := range rows {
			ev := r.event()
			// Publish on the row's originating trace, preserving tenant attribution.
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

// Purge uses the database clock to remove published outbox history older than a
// week, except rows retained by dead letters. Unpublished rows remain recoverable;
// old handled marks remain while an outbox or terminal record needs them.
// Dead letters require explicit operator action and are never purged here.
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

func relayRows(tx db.Tx[db.System], app appname.Name) ([]row, error) {
	var rows []row
	// Keep optional attribution nullable and trace members empty when absent.
	// FOR UPDATE OF o locks only outbox rows, leaving tenant resolution free.
	const q = `SELECT o.id, o.tenant_id, o.name, o.payload, o.created_at, o.actor,
		COALESCE(o.traceparent, '') AS traceparent, COALESCE(o.tracestate, '') AS tracestate,
		COALESCE(o.baggage, '') AS baggage, COALESCE(o.request_id, '') AS request_id,
		COALESCE(host(o.client_ip), '') AS client_ip,
		o.actor_kind, o.source_file, o.source_line, o.initiator, o.operation_id, o.operation_ref, o.operation_tenant_id, o.operation_person_id, o.operation_outcome, o.operation_revision FROM ` + table + ` o
		LEFT JOIN tenants tn ON tn.id = o.tenant_id
		WHERE o.published_at IS NULL AND coalesce(tn.app, '') = ?
		ORDER BY o.created_at, o.id LIMIT ? FOR UPDATE OF o SKIP LOCKED`
	if err := tx.DB().Raw(q, app.String(), batch).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("events: relay: read the outbox: %w", err)
	}
	return rows, nil
}

func observeRelayLag(ctx context.Context, tx db.Tx[db.System], rows []row) error {
	held := make(map[lagKey]struct{}, len(rows))
	for _, r := range rows {
		held[lagKey{tenant: r.TenantID, event: r.Name}] = struct{}{}
	}
	// Settle stale gauges only on a short batch, before observing new lag.
	// Full batches have not reached the queue's end and cannot prove it drained.
	if len(rows) < batch {
		if err := reportedSeries.settleDrained(ctx, tx, held); err != nil {
			return err
		}
	}
	if len(rows) == 0 {
		return nil
	}
	// Observe each tenant/event series once, using its oldest held row.
	oldestPerSeries(rows, func(r row) {
		telemetry.Shared().ObserveOutboxLag(ctx, db.Now().Sub(r.CreatedAt).Seconds(),
			attribute.String(telemetry.AttrTenantID, r.TenantID.String()),
			attribute.String("pkit.event", r.Name))
		reportedSeries.remember(lagKey{tenant: r.TenantID, event: r.Name})
	})
	return nil
}

func (r row) event() Event {
	ev := Event{ID: r.ID, Name: r.Name, TenantID: r.TenantID, Payload: r.Payload, At: r.CreatedAt,
		TraceParent: r.TraceParent, TraceState: r.TraceState, Baggage: r.Baggage,
		RequestID: r.RequestID, ClientIP: r.ClientIP}
	if r.OperationID != nil {
		ev.Operation.ID = *r.OperationID
	}
	if r.OperationRef != nil {
		ev.Operation.Ref = r.OperationRef.String()
	}
	if r.OperationTenantID != nil {
		ev.Operation.Tenant = r.OperationTenantID.String()
	}
	if r.OperationPersonID != nil {
		ev.Operation.Person = r.OperationPersonID.String()
	}
	if r.OperationOutcome != nil {
		ev.Operation.Outcome = *r.OperationOutcome
	}
	if r.OperationRevision != nil {
		ev.Operation.Revision = *r.OperationRevision
	}
	if r.Actor != nil {
		ev.Actor = *r.Actor
	}
	// The attribution the publisher stored, carried onto the envelope so a
	// subscriber three hops away can still say which file asked for this.
	if r.ActorKind != nil {
		ev.ActorKind = *r.ActorKind
	}
	if r.SourceFile != nil {
		ev.SourceFile = *r.SourceFile
	}
	if r.SourceLine != nil {
		ev.SourceLine = *r.SourceLine
	}
	if r.Initiator != nil {
		ev.Initiator = *r.Initiator
	}
	return ev
}
