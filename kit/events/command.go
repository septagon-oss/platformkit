package events

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/idempotency"
	"github.com/septagon-oss/platformkit/kit/request"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// AdmitCommand reserves a root command before execution. Scope names the
// code-owned command and trusted caller; person is an authenticated initiator,
// never a seed's impersonated principal. Empty Key gets a fresh ID. Nil clock
// resolves to db.Now. The returned context belongs to this occurrence only.
func AdmitCommand(ctx context.Context, conn *db.Conn, scope idempotency.Scope, person uuid.UUID, clock func() time.Time) (context.Context, error) {
	if clock == nil {
		clock = db.Now
	}
	t, _ := tenancy.FromContext(ctx)
	if t.ID == uuid.Nil || scope.Tenant != t.ID || scope.Route == "" {
		return nil, crud.ErrInvalid
	}
	scope.Method = "COMMAND"
	scope.Track = true
	delivery, fromDelivery := ctx.Value(commandDeliveryKey{}).(commandDelivery)
	if fromDelivery {
		scope.Caller = uuid.Nil
		person = uuid.Nil
		scope.Key = "event." + delivery.event.ID.String()
		scope.Route = delivery.durable + "/" + scope.Route
		input := append([]byte(delivery.event.Name+"\x00"), delivery.event.Payload...)
		input = append(input, scope.Fingerprint[:]...)
		scope.Fingerprint = sha256.Sum256(input)
	}
	if scope.Key == "" {
		scope.Key = uuid.NewString()
	}
	if !request.ValidToken(scope.Key) {
		return nil, crud.ErrInvalid
	}
	store := idempotency.Postgres(func(context.Context) (*db.Conn, bool) { return conn, conn != nil })
	v, err := store.Claim(ctx, scope, clock().UTC().Truncate(time.Microsecond))
	if err != nil {
		return nil, err
	}
	if v.Kind != idempotency.First {
		if fromDelivery && v.OperationRef != uuid.Nil && (v.Kind == idempotency.InFlight || v.Kind == idempotency.Spent) {
			var prior idempotency.Receipt
			err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				var err error
				prior, err = idempotency.Origin(tx, v.OperationRef)
				return err
			})
			if err != nil {
				return nil, err
			}
			if prior.Outcome == "" || prior.Outcome == request.PendingApproval {
				return request.WithOperationClock(ctx, prior.Operation(), clock), nil
			}
		}
		return nil, fmt.Errorf("%w: command occurrence already admitted", crud.ErrConflict)
	}
	r := idempotency.Receipt{Ref: v.OperationRef, ID: scope.Key, Tenant: scope.Tenant, Caller: scope.Caller, Person: person, Method: "COMMAND", Route: scope.Route, Revision: 1, StartedAt: v.StartedAt}
	c, cancel := context.WithTimeout(db.Detached(context.WithoutCancel(ctx)), 5*time.Second)
	err = db.Run(c, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return idempotency.PostgresOperations().Reserve(ctx, tx, r)
	})
	cancel()
	if err != nil {
		return nil, err
	}
	if err := store.Begin(ctx, scope, v.Claim, clock()); err != nil {
		return nil, err
	}
	return request.WithOperationClock(ctx, r.Operation(), clock), nil
}

// FailOperation records a definite rollback under the originating tenant. It
// preserves a terminal receipt, including success after an uncertain response.
// The caller must never use it for an uncertain COMMIT or provider result.
func FailOperation(ctx context.Context, conn *db.Conn, at time.Time) error {
	op, ok := request.OperationFrom(ctx)
	if !ok {
		return nil
	}
	tenant, err := uuid.Parse(op.Tenant)
	if err != nil {
		return err
	}
	ctx = tenancy.WithTenant(db.Detached(context.WithoutCancel(ctx)), tenancy.Tenant{ID: tenant})
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		r, err := idempotency.PostgresOperations().Current(ctx, tx, op)
		if err != nil {
			return err
		}
		if r.Outcome == request.Succeeded || r.Outcome == request.Failed {
			return nil
		}
		return RecordOutcome(ctx, tx, r.Revision, request.Failed, at)
	})
}

// RunOperation owns a root tenant command's admission and transaction. Nested
// commands reuse validated causality. Errors from the command prove rollback;
// commit errors remain unknown. State and outcome events commit together.
func RunOperation(ctx context.Context, conn *db.Conn, scope idempotency.Scope, person uuid.UUID, clock func() time.Time, run func(context.Context, db.Tx[db.Tenant]) error) error {
	if clock == nil {
		clock = db.Now
	}
	if op, ok := request.OperationFrom(ctx); ok {
		return db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			if _, err := idempotency.PostgresOperations().Current(ctx, tx, op); err != nil {
				return err
			}
			return run(ctx, tx)
		})
	}
	ctx, err := AdmitCommand(ctx, conn, scope, person, clock)
	if err != nil {
		return err
	}
	_, fromDelivery := ctx.Value(commandDeliveryKey{}).(commandDelivery)
	refused := false
	defer func() {
		if value := recover(); value != nil {
			if !fromDelivery {
				_ = FailOperation(ctx, conn, clock())
			}
			panic(value)
		}
	}()
	err = db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := run(ctx, tx); err != nil {
			refused = true
			return err
		}
		op, _ := request.OperationFrom(ctx)
		r, currentErr := idempotency.PostgresOperations().Current(ctx, tx, op)
		if currentErr != nil {
			refused = true
			return currentErr
		}
		if r.Outcome != "" {
			return nil
		}
		if err := Succeed(ctx, tx, clock()); err != nil {
			refused = true
			return err
		}
		return nil
	})
	if err != nil && refused && !fromDelivery {
		if evidence := FailOperation(ctx, conn, clock()); evidence != nil {
			return fmt.Errorf("operation failure evidence: %w", evidence)
		}
	}
	return err
}

// ProvisionOperation names a new tenant's provisioning work inside its creator's
// authoritative transaction. Until that transaction commits, the new tenant and
// this child intent have never been accepted. It carries no person projection.
func ProvisionOperation(ctx context.Context, tx db.Tx[db.Tenant], name string, clock func() time.Time) (context.Context, error) {
	if clock == nil {
		clock = db.Now
	}
	if name == "" {
		return nil, crud.ErrInvalid
	}
	at := clock().UTC().Truncate(time.Microsecond)
	r := idempotency.Receipt{Ref: uuid.New(), ID: uuid.NewString(), Tenant: db.TenantOf(tx).ID, Method: "COMMAND", Route: name, Revision: 1, StartedAt: at}
	if err := idempotency.PostgresOperations().Reserve(ctx, tx, r); err != nil {
		return nil, err
	}
	return request.WithOperationClock(ctx, r.Operation(), clock), nil
}

type commandDeliveryKey struct{}
type commandDelivery struct {
	event   Event
	durable string
}
