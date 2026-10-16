package idempotency

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/request"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

const operationsTable = "platformkit_operations"

// Receipt is the minimal record of an admitted operation. Empty Outcome and zero
// OutcomeAt mean unknown, including accepted work still awaiting its owner.
type Receipt struct {
	Ref       uuid.UUID       `json:"ref"`
	ID        string          `json:"id"`
	Tenant    uuid.UUID       `json:"-"`
	Caller    uuid.UUID       `json:"-"`
	Person    uuid.UUID       `json:"-"`
	Method    string          `json:"method"`
	Route     string          `json:"route"`
	Outcome   request.Outcome `json:"outcome,omitempty"`
	Revision  int64           `json:"revision"`
	StartedAt time.Time       `json:"startedAt"`
	OutcomeAt time.Time       `json:"outcomeAt,omitzero"`
}

// Operation returns the receipt's portable causality snapshot.
func (r Receipt) Operation() request.Operation {
	person := ""
	if r.Person != uuid.Nil {
		person = r.Person.String()
	}
	return request.Operation{ID: r.ID, Ref: r.Ref.String(), Tenant: r.Tenant.String(), Person: person, Outcome: r.Outcome, Revision: r.Revision}
}

// Operations records outcomes inside the authoritative tenant transaction.
// Transition returns no receipt on refusal and reports whether an event is owed.
// Memory implements the same decisions, without simulating database rollback.
type Operations interface {
	Reserve(context.Context, db.Tx[db.Tenant], Receipt) error
	Get(context.Context, db.Tx[db.Tenant], uuid.UUID, uuid.UUID, time.Time) (Receipt, error)
	Current(context.Context, db.Tx[db.Tenant], request.Operation) (Receipt, error)
	Transition(context.Context, db.Tx[db.Tenant], request.Operation, int64, request.Outcome, time.Time) (Receipt, bool, error)
}

// PostgresOperations uses the caller's transaction and the kernel receipt table.
func PostgresOperations() Operations { return operationSQL{} }

type operationSQL struct{}

// operationRow uses Base's id as the immutable operation reference so receipt
// locking follows the same aggregate-root path as other tenant entities.
type operationRow struct {
	crud.Base
	OperationID string
	CallerID    uuid.UUID
	PersonID    *uuid.UUID
	Method      string
	Route       string
	Outcome     *request.Outcome
	Revision    int64
	StartedAt   time.Time
	OutcomeAt   *time.Time
}

func (*operationRow) TableName() string { return operationsTable }
func (r *operationRow) receipt() Receipt {
	v := Receipt{Ref: r.ID, ID: r.OperationID, Tenant: r.TenantID, Caller: r.CallerID, Method: r.Method, Route: r.Route, Revision: r.Revision, StartedAt: r.StartedAt}
	if r.PersonID != nil {
		v.Person = *r.PersonID
	}
	if r.Outcome != nil {
		v.Outcome = *r.Outcome
	}
	if r.OutcomeAt != nil {
		v.OutcomeAt = *r.OutcomeAt
	}
	return v
}

func reservationTime(r Receipt) Receipt {
	r.StartedAt = r.StartedAt.UTC().Truncate(time.Microsecond)
	return r
}

func validReservation(r Receipt, tenant uuid.UUID) error {
	if tenant == uuid.Nil || r.Tenant != tenant || r.Ref == uuid.Nil || !request.ValidToken(r.ID) || r.Method == "" || r.Route == "" || r.StartedAt.IsZero() || r.Revision != 1 || r.Outcome != "" || !r.OutcomeAt.IsZero() {
		return fmt.Errorf("%w: invalid operation reservation", crud.ErrInvalid)
	}
	return nil
}

func sameReservation(a, b Receipt) bool {
	return a.Ref == b.Ref && a.ID == b.ID && a.Tenant == b.Tenant && a.Caller == b.Caller && a.Person == b.Person && a.Method == b.Method && a.Route == b.Route && a.StartedAt.Equal(b.StartedAt)
}

func (operationSQL) Reserve(_ context.Context, tx db.Tx[db.Tenant], r Receipt) error {
	r = reservationTime(r)
	if err := validReservation(r, db.TenantOf(tx).ID); err != nil {
		return err
	}
	res := tx.DB().Exec(`INSERT INTO platformkit_operations
		(id, tenant_id, operation_id, caller_id, person_id, method, route, revision, started_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?) ON CONFLICT (tenant_id, id) DO NOTHING`,
		r.Ref, r.Tenant, r.ID, r.Caller, nullableUUID(r.Person), r.Method, r.Route, r.StartedAt)
	if res.Error != nil {
		return res.Error
	}
	var row operationRow
	if err := tx.DB().Where("id = ?", r.Ref).Take(&row).Error; err != nil {
		return err
	}
	if !sameReservation(row.receipt(), r) {
		return fmt.Errorf("%w: operation reference already bound", crud.ErrConflict)
	}
	return nil
}

func expired(r Receipt, at time.Time) bool {
	return (r.Outcome == request.Succeeded || r.Outcome == request.Failed) && !at.Before(maxTime(r.StartedAt, r.OutcomeAt).Add(retention))
}
func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func (operationSQL) Get(_ context.Context, tx db.Tx[db.Tenant], ref, person uuid.UUID, at time.Time) (Receipt, error) {
	if at.IsZero() {
		return Receipt{}, fmt.Errorf("%w: operation read needs an instant", crud.ErrInvalid)
	}
	if person == uuid.Nil {
		return Receipt{}, crud.ErrNotFound
	}
	var row operationRow
	err := tx.DB().Where("id = ? AND person_id = ?", ref, person).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Receipt{}, crud.ErrNotFound
	}
	if err != nil {
		return Receipt{}, err
	}
	r := row.receipt()
	if expired(r, at) {
		return Receipt{}, crud.ErrNotFound
	}
	return r, nil
}

func bound(r Receipt, op request.Operation) bool {
	return r.Operation().ID == op.ID && r.Operation().Ref == op.Ref && r.Operation().Tenant == op.Tenant && r.Operation().Person == op.Person
}

func (operationSQL) Current(_ context.Context, tx db.Tx[db.Tenant], op request.Operation) (Receipt, error) {
	ref, err := uuid.Parse(op.Ref)
	if err != nil || op.Tenant != db.TenantOf(tx).ID.String() {
		return Receipt{}, crud.ErrNotFound
	}
	var row operationRow
	if err := tx.DB().Where("id = ?", ref).Take(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Receipt{}, crud.ErrNotFound
		}
		return Receipt{}, err
	}
	r := row.receipt()
	if !bound(r, op) {
		return Receipt{}, crud.ErrNotFound
	}
	return r, nil
}

// decideOutcome is shared by SQL and Memory. Revision and clock checks precede
// the no-op check, so an old snapshot cannot receive the current row on refusal.
func decideOutcome(r Receipt, expected int64, outcome request.Outcome, at time.Time) (Receipt, bool, error) {
	if expected <= 0 || !outcome.Valid() || at.IsZero() || at.Before(r.StartedAt) || (!r.OutcomeAt.IsZero() && at.Before(r.OutcomeAt)) {
		return Receipt{}, false, fmt.Errorf("%w: invalid operation outcome or instant", crud.ErrInvalid)
	}
	if r.Revision != expected {
		return Receipt{}, false, fmt.Errorf("%w: stale operation revision", crud.ErrConflict)
	}
	if r.Outcome == outcome {
		return r, false, nil
	}
	if r.Outcome == request.Succeeded || r.Outcome == request.Failed {
		return Receipt{}, false, fmt.Errorf("%w: terminal operation outcome", crud.ErrConflict)
	}
	r.Outcome, r.OutcomeAt, r.Revision = outcome, at.UTC().Truncate(time.Microsecond), r.Revision+1
	return r, true, nil
}

func (operationSQL) Transition(_ context.Context, tx db.Tx[db.Tenant], op request.Operation, expected int64, outcome request.Outcome, at time.Time) (Receipt, bool, error) {
	ref, err := uuid.Parse(op.Ref)
	if err != nil || op.Tenant != db.TenantOf(tx).ID.String() {
		return Receipt{}, false, crud.ErrNotFound
	}
	row, err := crud.GetForUpdate[*operationRow](tx, ref)
	if err != nil {
		return Receipt{}, false, err
	}
	if !bound(row.receipt(), op) || expired(row.receipt(), at) {
		return Receipt{}, false, crud.ErrNotFound
	}
	r, changed, err := decideOutcome(row.receipt(), expected, outcome, at)
	if err != nil || !changed {
		return r, changed, err
	}
	res := tx.DB().Exec(`UPDATE platformkit_operations SET outcome=?, outcome_at=?, revision=? WHERE id=? AND revision=?`, r.Outcome, r.OutcomeAt, r.Revision, ref, expected)
	if res.Error != nil {
		return Receipt{}, false, res.Error
	}
	if res.RowsAffected != 1 {
		return Receipt{}, false, crud.ErrConflict
	}
	return r, true, nil
}

func nullableUUID(id uuid.UUID) any {
	if id == uuid.Nil {
		return nil
	}
	return id
}

// Origin reads a server-owned continuation link in this tenant. It grants no
// HTTP read authority; a person projection must use Operations.Get instead.
func Origin(tx db.Tx[db.Tenant], ref uuid.UUID) (Receipt, error) {
	var row operationRow
	err := tx.DB().Where("id = ?", ref).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Receipt{}, crud.ErrNotFound
	}
	if err != nil {
		return Receipt{}, err
	}
	return row.receipt(), nil
}

// LockReceipts orders receipt locks after the owner's domain locks. The named
// transaction lock serializes commands that complete a set of tenant receipts.
func LockReceipts(tx db.Tx[db.Tenant], refs ...uuid.UUID) error {
	if len(refs) > 1 {
		if err := tx.DB().Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, "platformkit.operation-set."+db.TenantOf(tx).ID.String()).Error; err != nil {
			return err
		}
	}
	refs = slices.Clone(refs)
	slices.SortFunc(refs, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })
	for _, ref := range slices.Compact(refs) {
		if _, err := crud.GetForUpdate[*operationRow](tx, ref); err != nil {
			return err
		}
	}
	return nil
}

// SucceedSystem stages an initiating tenant's receipt inside an existing
// authorized control-plane transaction. It creates no system capability.
func SucceedSystem(ctx context.Context, tx db.Tx[db.System], at time.Time) (Receipt, bool, error) {
	op, ok := request.OperationFrom(ctx)
	if !ok || request.OperationDeferred(ctx) {
		return Receipt{}, false, nil
	}
	if _, err := uuid.Parse(op.Tenant); err != nil {
		return Receipt{}, false, crud.ErrNotFound
	}
	if _, err := uuid.Parse(op.Ref); err != nil {
		return Receipt{}, false, crud.ErrNotFound
	}
	var row operationRow
	err := tx.DB().Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id=? AND id=?", op.Tenant, op.Ref).Take(&row).Error
	if err != nil {
		return Receipt{}, false, err
	}
	if !bound(row.receipt(), op) {
		return Receipt{}, false, crud.ErrNotFound
	}
	if row.Outcome != nil && *row.Outcome == request.Succeeded {
		return row.receipt(), false, nil
	}
	r, changed, err := decideOutcome(row.receipt(), row.Revision, request.Succeeded, at)
	if err != nil || !changed {
		return r, changed, err
	}
	err = tx.DB().Exec(`UPDATE platformkit_operations SET outcome=?,outcome_at=?,revision=? WHERE tenant_id=? AND id=? AND revision=?`, r.Outcome, r.OutcomeAt, r.Revision, r.Tenant, r.Ref, row.Revision).Error
	if err != nil {
		return Receipt{}, false, err
	}
	return r, true, nil
}

func memoryTenant(ctx context.Context, tx db.Tx[db.Tenant]) uuid.UUID {
	if tx.DB() != nil {
		return db.TenantOf(tx).ID
	}
	t, _ := tenancy.FromContext(ctx)
	return t.ID
}

func (m *memory) Reserve(ctx context.Context, tx db.Tx[db.Tenant], r Receipt) error {
	r = reservationTime(r)
	if err := validReservation(r, memoryTenant(ctx, tx)); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if old, ok := m.operations[operationKey{r.Tenant, r.Ref}]; ok {
		if !sameReservation(old, r) {
			return crud.ErrConflict
		}
		return nil
	}
	m.operations[operationKey{r.Tenant, r.Ref}] = r
	return nil
}
func (m *memory) Get(ctx context.Context, tx db.Tx[db.Tenant], ref, person uuid.UUID, at time.Time) (Receipt, error) {
	if at.IsZero() {
		return Receipt{}, crud.ErrInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.operations[operationKey{memoryTenant(ctx, tx), ref}]
	if !ok || person == uuid.Nil || r.Person != person || r.Tenant != memoryTenant(ctx, tx) || expired(r, at) {
		return Receipt{}, crud.ErrNotFound
	}
	if at.IsZero() {
		return Receipt{}, crud.ErrInvalid
	}
	return r, nil
}
func (m *memory) Current(ctx context.Context, tx db.Tx[db.Tenant], op request.Operation) (Receipt, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.current(ctx, tx, op)
}
func (m *memory) current(ctx context.Context, tx db.Tx[db.Tenant], op request.Operation) (Receipt, error) {
	ref, err := uuid.Parse(op.Ref)
	r, ok := m.operations[operationKey{memoryTenant(ctx, tx), ref}]
	if err != nil || !ok || r.Tenant != memoryTenant(ctx, tx) || !bound(r, op) {
		return Receipt{}, crud.ErrNotFound
	}
	return r, nil
}
func (m *memory) Transition(ctx context.Context, tx db.Tx[db.Tenant], op request.Operation, expected int64, outcome request.Outcome, at time.Time) (Receipt, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, err := m.current(ctx, tx, op)
	if err != nil {
		return Receipt{}, false, err
	}
	if expired(r, at) {
		return Receipt{}, false, crud.ErrNotFound
	}
	r, changed, err := decideOutcome(r, expected, outcome, at)
	if changed {
		m.operations[operationKey{r.Tenant, r.Ref}] = r
	}
	return r, changed, err
}
