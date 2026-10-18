package events

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events/transport"
	"github.com/septagon-oss/platformkit/kit/idempotency"
	"github.com/septagon-oss/platformkit/kit/request"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// EventOperationOutcome records a changed authoritative outcome, not a domain creation.
const EventOperationOutcome = "platformkit.operation_outcome"

// OperationOutcome is the receipt revision observers must reconcile.
type OperationOutcome struct {
	Ref      uuid.UUID       `json:"ref"`
	Revision int64           `json:"revision"`
	Outcome  request.Outcome `json:"outcome"`
}

// RecordOutcome transitions the admitted intent at expected revision and emits
// its evidence in the same transaction. Identical current outcomes emit nothing.
// A caller must propagate errors so its authoritative write rolls back.
func RecordOutcome(ctx context.Context, tx db.Tx[db.Tenant], expected int64, outcome request.Outcome, at time.Time) error {
	op, ok := request.OperationFrom(ctx)
	if !ok {
		return nil
	}
	r, changed, err := idempotency.PostgresOperations().Transition(ctx, tx, op, expected, outcome, at)
	if err != nil || !changed {
		return err
	}
	ctx = request.WithOperation(ctx, r.Operation())
	return Publish(ctx, tx, EventOperationOutcome, OperationOutcome{Ref: r.Ref, Revision: r.Revision, Outcome: r.Outcome})
}

// Succeed stages completion before an owner's authoritative successful event.
// Deferred work and historical callers without admission are left unchanged.
func Succeed(ctx context.Context, tx db.Tx[db.Tenant], at time.Time) error {
	op, ok := request.OperationFrom(ctx)
	if !ok || request.OperationDeferred(ctx) {
		return nil
	}
	r, err := idempotency.PostgresOperations().Current(ctx, tx, op)
	if err != nil {
		return err
	}
	if r.Outcome == request.Succeeded {
		return nil
	}
	return RecordOutcome(ctx, tx, r.Revision, request.Succeeded, request.OperationTime(ctx, at))
}

// SucceedFor stages success in the owner's existing control-plane transaction,
// including its outcome event under the explicit initiating operator tenant.
func SucceedFor(ctx context.Context, tx db.Tx[db.System], at time.Time) error {
	r, changed, err := idempotency.SucceedSystem(ctx, tx, request.OperationTime(ctx, at))
	if err != nil || !changed {
		return err
	}
	return PublishFor(request.WithOperation(ctx, r.Operation()), tx, r.Tenant, EventOperationOutcome, OperationOutcome{Ref: r.Ref, Revision: r.Revision, Outcome: r.Outcome})
}

// operationSnapshot rechecks identity against durable state without trusting an
// envelope's outcome. Expired terminal receipts retain only their carried fact.
func operationSnapshot(ctx context.Context, gdb *gorm.DB, tenant uuid.UUID, system bool) (request.Operation, error) {
	op, ok := request.OperationFrom(ctx)
	if !ok {
		return request.Operation{}, nil
	}
	if err := transport.ValidateOperation(op); err != nil {
		return request.Operation{}, err
	}
	if !system && op.Tenant != tenant.String() {
		delivery, carried := ctx.Value(commandDeliveryKey{}).(commandDelivery)
		if carried && delivery.event.TenantID == tenant && delivery.event.Operation == op && (op.Outcome == request.Succeeded || op.Outcome == request.Failed) {
			return op, nil
		}
		return request.Operation{}, crud.ErrNotFound
	}
	var row struct {
		OperationID string
		PersonID    *uuid.UUID
		Outcome     *request.Outcome
		Revision    int64
	}
	err := gdb.Table("platformkit_operations").Where("tenant_id = ? AND id = ?", op.Tenant, op.Ref).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) && (op.Outcome == request.Succeeded || op.Outcome == request.Failed) {
		return op, nil
	}
	if err != nil {
		return request.Operation{}, err
	}
	person := ""
	if row.PersonID != nil {
		person = row.PersonID.String()
	}
	if row.OperationID != op.ID || person != op.Person {
		return request.Operation{}, crud.ErrNotFound
	}
	op.Revision = row.Revision
	op.Outcome = ""
	if row.Outcome != nil {
		op.Outcome = *row.Outcome
	}
	return op, nil
}

func operationColumns(op request.Operation) []any {
	if op.ID == "" {
		return []any{nil, nil, nil, nil, nil, nil}
	}
	var person, outcome any
	if op.Person != "" {
		person = op.Person
	}
	if op.Outcome != "" {
		outcome = string(op.Outcome)
	}
	return []any{op.ID, op.Ref, op.Tenant, person, outcome, op.Revision}
}

func terminalLetter(ctx context.Context, conn *db.Conn, ev Event, durable string, cause error, app appname.Name, terminal func(context.Context, db.Tx[db.Tenant], Event, error) error) error {
	if err := transport.ValidateOperation(ev.Operation); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, handlerTimeout)
	defer cancel()
	holds, err := holdsTenant(ctx, conn, app, ev.TenantID)
	if err != nil || !holds {
		return err
	}
	ctx = tenancy.WithTenant(request.WithOperation(ctx, ev.Operation), tenancy.Tenant{ID: ev.TenantID})
	request.DeferOperation(ctx)
	return db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if !app.Named() {
			holds, err := holdsUnscoped(ctx, tx, ev.TenantID)
			if err != nil || !holds {
				return err
			}
		}
		first, err := claim(tx, ev.ID, durable, "")
		if err != nil || !first {
			return err
		}
		ctx = context.WithValue(ctx, commandDeliveryKey{}, commandDelivery{event: ev, durable: durable})
		if err := terminal(ctx, tx, ev, cause); err != nil {
			return err
		}
		return tx.DB().Exec(`INSERT INTO platformkit_dead_letters (event_id,durable,tenant_id,name,error)
		 VALUES (?,?,?,?,?) ON CONFLICT DO NOTHING`, ev.ID, durable, ev.TenantID, ev.Name, cause.Error()).Error
	})
}
