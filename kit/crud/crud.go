// Package crud is the five operations every tenant-owned resource needs: a
// module writes a struct with an embedded Base and gets read, list, create,
// update and delete, each refusing what row-level security would refuse anyway.
//
// This half links no web server: kit/rest owns routes, PATCH merging and OpenAPI
// and imports this storage adapter. Instantiate with the pointer type —
// crud.Get[*Task](tx, id) — so the kernel can stamp the tenant.
//
// The tenant is never a parameter: Create takes it from the transaction and
// Update refuses an entity carrying a different one, which is defence in depth
// over the boundary the database enforces (docs/adr/0003). Which family of door to
// call, and what each asks of its caller, is in README.md; the [Get] and [List]
// examples call the two reads from inside a tenant transaction of their own.
package crud

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/fault"
)

// The three failures a caller distinguishes. What the mapping does not
// recognise is an outage and reads as a 500; the authorization refusals
// kit/tenancy declares are the ones answered a 403 or a 503. Spec.Mount turns
// these into 404, 422 and 409.
//
// These are kit/fault's values, named again here, and not copies of them. A
// refusal has to be one object: kit/rest maps the three to a status once, and
// the value package that refuses a write before a transaction exists and this
// adapter, which classifies one after the database refused it, have to hold the
// same opinion for that one mapping to answer for both. So a package that takes
// no transaction wraps kit/fault, and a caller that already wrote crud.Err…
// keeps matching it with errors.Is.
var (
	// ErrNotFound is no such row in this tenant. Another tenant's row is not
	// found either, which is the only thing the API may say about it.
	ErrNotFound = fault.ErrNotFound
	// ErrInvalid is the entity's own Validate, or a query naming a field that
	// does not exist.
	ErrInvalid = fault.ErrInvalid
	// ErrConflict is a write that contradicts existing data or business state.
	ErrConflict = fault.ErrConflict
)

// UniqueConflict identifies a duplicate value without confusing it with a
// business-state refusal. Constraint is for diagnostics, not a public field name
// or message. Callers can still match ErrConflict with errors.Is.
type UniqueConflict struct {
	Constraint string
}

func (e *UniqueConflict) Error() string { return ErrConflict.Error() + ": " + e.Constraint }
func (e *UniqueConflict) Unwrap() error { return ErrConflict }

// Base is the entity's identity, tenancy and lifecycle metadata.
// It is an alias so existing entities remain usable by kit/entity consumers.
type Base = entity.Base

// Entity is a tenant-owned row with the embedded Base.
type Entity = entity.Entity

// Validator is the optional check run before a storage write.
type Validator = entity.Validator

// Query is a list request: a page, an order, a set of equality filters and a set of
// comparisons. Sort is "field" or "-field" and every name is checked against the
// entity's schema, so a column name never comes from a caller.
//
// Filter and Where narrow together: one row is on the page when the lifecycle clause
// and every equality key and every predicate hold. A field named by both is narrowed
// twice rather than overwritten, and a pair that contradicts answers an empty page
// rather than a refusal. Where is built by server code from a declaration or a
// command the module checked; no request parameter of this repository reaches it.
type Query struct {
	Limit, Offset int
	Sort          string

	// Filter is one value per field name, each read as that column's own Go type by
	// the rule Predicate states: any integer kind for a count, any float or integer
	// kind for an amount, and exactly string, bool, time.Time or uuid.UUID for a
	// word, a flag, a moment and an id. A value of another shape refuses the whole
	// question with ErrInvalid before a statement is sent; none of it is coerced. A
	// value of the right shape and a number too wide for the column is the driver's
	// refusal and not ErrInvalid: the rule is on a shape, and a column's width is no
	// fact this package reads. Where takes the same rule.
	Filter map[string]any

	// Where is the comparisons of the same question, narrowed with Filter and bound
	// by the same rule on an operand's shape.
	Where Where
}

// The page and operand bounds. A caller that asks for nothing gets a screenful; a
// caller that asks for everything gets the most a single response should carry.
const (
	DefaultLimit = 50
	MaxLimit     = 200

	// MaxInValues is how many values one In predicate carries. It is MaxLimit
	// because the set a caller lists is bounded by the page it came from: a page
	// reads at most MaxLimit rows, so a list assembled from rows never needs more,
	// and a caller holding a longer list is asking a question a page cannot answer.
	MaxInValues = 200

	// MaxQueryValues is how many values one question carries in total — every
	// equality key plus every predicate operand, at the total and the page built
	// from the same list. Postgres counts the parameters of a statement in sixteen
	// bits, so this ceiling is the product's own, set far under the wire's: a
	// question that trips it is a bug, not a tuning request.
	MaxQueryValues = 2000
)

// Get reads one row of this tenant. A soft-deleted row is not found.
//
// Deprecated: Use GetCtx, which answers the caller's own ending before it sends a
// statement instead of reading a row nobody is waiting for.
func Get[T Entity](tx db.Tx[db.Tenant], id uuid.UUID) (T, error) {
	return GetCtx[T](context.Background(), tx, id)
}

// GetCtx reads one row of this tenant, or ErrNotFound — including another tenant's
// row, which is not found either, the only thing the API may say about it. A
// soft-deleted row is not found.
//
// ctx is the caller's own cancellation: it is checked before anything reaches the
// database, and the ended caller is an error wrapping its cause, not an ErrNotFound
// for a row that exists. It never rebinds the statement, which runs on the handle
// the transaction gave the caller — the handle already carries the caller's deadline,
// and a door of this package that asks its caller gets that handle only from [begin].
func GetCtx[T Entity](ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (T, error) {
	q, err := begin(ctx, tx)
	if err != nil {
		var zero T
		return zero, err
	}
	return get[T](q, id)
}

// GetForUpdate reads and locks one live row of this tenant until the caller's
// transaction ends. Use it before decisions or validation that depend on the
// stored state. At read committed, a waiter reads the preceding writer's
// committed version. Serialization failures at stricter isolation levels pass
// through unchanged; the caller must retry its whole transaction.
func GetForUpdate[T Entity](tx db.Tx[db.Tenant], id uuid.UUID) (T, error) {
	return get[T](tx.DB().Clauses(clause.Locking{Strength: "UPDATE"}), id)
}

func get[T Entity](query *gorm.DB, id uuid.UUID) (T, error) {
	e := blank[T]()
	if err := query.Where("id = ? AND deleted_at IS NULL", id).Take(e).Error; err != nil {
		var zero T
		return zero, Classify(err)
	}
	return e, nil
}

// Count returns the total live rows visible to this tenant without loading a page.
//
// Deprecated: Use CountCtx, which answers the caller's own ending before it sends a
// statement.
func Count[T Entity](tx db.Tx[db.Tenant]) (int64, error) {
	return CountCtx[T](context.Background(), tx)
}

// CountCtx returns the total live rows visible to this tenant without loading a
// page. ctx is checked before the statement and rebinds nothing — see GetCtx.
func CountCtx[T Entity](ctx context.Context, tx db.Tx[db.Tenant]) (int64, error) {
	q, err := begin(ctx, tx)
	if err != nil {
		return 0, err
	}
	var total int64
	err = q.Model(blank[T]()).Where("deleted_at IS NULL").Count(&total).Error
	return total, Classify(err)
}

// List reads a page of this tenant's rows and the total the page came from.
// The order always ends in the id, so two pages of equal-keyed rows do not
// overlap or skip.
//
// Deprecated: Use ListCtx, which answers the caller's own ending before it sends a
// statement.
func List[T Entity](tx db.Tx[db.Tenant], q Query) ([]T, int64, error) {
	return ListCtx[T](context.Background(), tx, q)
}

// ListCtx reads a page of this tenant's rows and the total the page came from. The
// order always ends in the id, so two pages of equal-keyed rows neither overlap nor
// skip. A filter, a predicate or a sort naming no field of the schema is refused
// ErrInvalid before the caller is asked and before anything connects — an
// unanswerable query is the caller's bug, and it is answered whatever the caller's
// own state. The total and the page are built from one set of conditions, so a
// narrowed page and the number under it are answers about one set of rows. ctx is
// checked before the statements and rebinds nothing — see GetCtx.
func ListCtx[T Entity](ctx context.Context, tx db.Tx[db.Tenant], q Query) ([]T, int64, error) {
	clauses, err := fragments(Fields[T](), q.Filter, q.Where)
	if err != nil {
		return nil, 0, err
	}
	order, err := ordering(Fields[T](), q.Sort)
	if err != nil {
		return nil, 0, err
	}
	stmt, err := begin(ctx, tx)
	if err != nil {
		return nil, 0, err
	}
	limit := q.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	limit = min(limit, MaxLimit)

	// The count and the page are built from separate statements rather than
	// from one reused handle: GORM carries clauses forward, and a Count that
	// inherits a LIMIT counts the page instead of the table.
	build := func() *gorm.DB {
		g := stmt.Model(blank[T]()).Where("deleted_at IS NULL")
		for _, c := range clauses {
			g = g.Where(c.sql, c.args...)
		}
		return g
	}
	var total int64
	if err := build().Count(&total).Error; err != nil {
		return nil, 0, Classify(err)
	}
	var out []T
	if err := build().Order(order).Limit(limit).Offset(max(q.Offset, 0)).Find(&out).Error; err != nil {
		return nil, 0, Classify(err)
	}
	return out, total, nil
}

// Create writes a new row, stamped with the transaction's tenant.
//
// An entity that already carries a different tenant is refused rather than
// restamped. Update refuses the same thing, and the two have to agree: code
// that reads a row in one tenant and creates it in another has a bug either
// way, and silently rewriting the field means the bug ships as a copy.
func Create[T Entity](ctx context.Context, tx db.Tx[db.Tenant], e T) error {
	if isNil(e) {
		return fmt.Errorf("%w: there is nothing to create", ErrInvalid)
	}
	b := entity.BaseOf(e)
	if b.ID == uuid.Nil {
		b.ID = uuid.New()
	}
	tenant := db.TenantOf(tx).ID
	if b.TenantID != uuid.Nil && b.TenantID != tenant {
		return fmt.Errorf("%w: this entity belongs to another tenant", ErrInvalid)
	}
	b.TenantID = tenant
	b.DeletedAt = nil
	if err := validate(ctx, e); err != nil {
		return err
	}
	if err := tx.DB().Create(e).Error; err != nil {
		return Classify(err)
	}
	return nil
}

// RecheckTenant is the tenant-scope recheck Update performs before it writes: an
// entity carrying a tenant that is not the transaction's is not this caller's row
// and is answered ErrNotFound, the only thing the API may say about it. A row with
// no tenant is the transaction's to stamp, which is what Update then does with it.
// Nothing is answered ErrInvalid, the answer for nothing to act on: an exported
// door cannot state a non-nil precondition its callers do not have.
//
// It is exported for a caller that read a row under the lock and then decided to
// write nothing, which is where row-level security stops being the backstop: on a
// table whose read policy shows one shared list to every tenant (docs/adr/0008),
// GetForUpdate answers a row a request may read and may not write, and a write
// that never happens is one WITH CHECK never gets to refuse. A patch naming no
// column is that write.
func RecheckTenant(tx db.Tx[db.Tenant], e Entity) error {
	if isNil(e) {
		return fmt.Errorf("%w: there is nothing to recheck", ErrInvalid)
	}
	b := entity.BaseOf(e)
	if b.ID == uuid.Nil {
		return ErrNotFound
	}
	if tenant := db.TenantOf(tx).ID; b.TenantID != uuid.Nil && b.TenantID != tenant {
		return ErrNotFound
	}
	return nil
}

// Update writes an existing row back, refusing an entity that carries another
// tenant — which row-level security would refuse too, and not found is the answer
// the read would have given.
//
// columns names the database columns to write; no columns means all of them. The
// distinction is what lets two concurrent PATCHes of different fields both
// survive: writing every column writes the first request's fields back to what
// they were when it read them, losing a change nobody touched. Spec.Mount passes
// exactly the columns the patch named, and crud.Schema is the only producer of
// those names.
//
// Read with GetForUpdate before merging or validating stored state: a lock taken
// only at the write cannot correct a decision made on a stale row. A failed write
// aborts the transaction, so a retry needs a new one.
func Update[T Entity](ctx context.Context, tx db.Tx[db.Tenant], e T, columns ...string) error {
	if isNil(e) {
		return fmt.Errorf("%w: there is nothing to update", ErrInvalid)
	}
	if err := RecheckTenant(tx, e); err != nil {
		return err
	}
	entity.BaseOf(e).TenantID = db.TenantOf(tx).ID // a row that carried no tenant is this one's to stamp
	if err := validate(ctx, e); err != nil {
		return err
	}
	// Updates and not Save: GORM's Save falls back to an INSERT when the
	// UPDATE matches no row, which for a row another tenant owns — invisible,
	// so zero rows — would be a resurrection rather than a refusal. Model
	// carries the primary key into the WHERE clause.
	q := tx.DB().Model(e).Where("deleted_at IS NULL")
	if len(columns) == 0 {
		// Select("*") writes every field; Omit protects the four the server owns.
		q = q.Select("*").Omit("id", "tenant_id", "created_at", "deleted_at")
	} else {
		q = q.Select(columns)
	}
	res := q.Updates(e)
	if res.Error != nil {
		return Classify(res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete removes a row: soft sets deleted_at, which hides it from Get and List
// while keeping it for anything that referenced it; otherwise the row goes.
//
// Deprecated: Use SoftDelete or DeleteRow, which name which of the two answers the
// caller asked for instead of a bare bool at the call site.
func Delete[T Entity](tx db.Tx[db.Tenant], id uuid.UUID, soft bool) error {
	if soft {
		return SoftDelete[T](context.Background(), tx, id)
	}
	return DeleteRow[T](context.Background(), tx, id)
}

// SoftDelete stamps deleted_at with db.Now — the instant the column keeps — on one
// live row of this tenant, which hides it from Get, GetCtx, Count and List while
// keeping it for anything that referenced it. No such row is ErrNotFound and writes
// nothing. ctx is checked before the statement and rebinds nothing — see GetCtx.
func SoftDelete[T Entity](ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) error {
	q, err := begin(ctx, tx)
	if err != nil {
		return err
	}
	return stampDeletion[T](q, id)
}

// DeleteRow removes one live row of this tenant from the table. Reference to it from
// another row is the caller's to have checked: the database's refusal comes back
// through Classify, and nothing about it is soft. No such row is ErrNotFound and
// writes nothing. A caller whose resource may still be pointed at wants SoftDelete.
// ctx is checked before the statement and rebinds nothing — see GetCtx.
func DeleteRow[T Entity](ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) error {
	q, err := begin(ctx, tx)
	if err != nil {
		return err
	}
	return eraseRow[T](q, id)
}

// stampDeletion and eraseRow are the two statements the two doors above send, and
// the only ones either answer comes from: a row that is not there, or not live, or
// another tenant's, is refused by row-level security as zero rows affected.
func stampDeletion[T Entity](query *gorm.DB, id uuid.UUID) error {
	res := query.Model(blank[T]()).
		Where("id = ? AND deleted_at IS NULL", id).
		Update("deleted_at", db.Now())
	return deleted(res)
}

func eraseRow[T Entity](query *gorm.DB, id uuid.UUID) error {
	e := blank[T]()
	res := query.Model(e).Where("id = ? AND deleted_at IS NULL", id).Delete(e)
	return deleted(res)
}

// deleted is the one answer both statements give: the database's own failure
// classified, and nothing touched reported as no such row.
func deleted(res *gorm.DB) error {
	if res.Error != nil {
		return Classify(res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// begin asks a caller that has already ended, in the words that name it, and hands
// back the statement handle the transaction gave that caller. The two belong in one
// call: the refusal is neither ErrNotFound — a row nobody waited for still exists —
// nor ErrInvalid, which would tell the caller to correct a submission it never sent,
// and the handle comes from the transaction rather than from this context, which the
// query never sees. A door therefore takes the caller's ending as a question and the
// transaction's scope as its scope; rebinding a statement to the caller would replace
// the one scope that carries the tenant settings the row-level security reads.
func begin(ctx context.Context, tx db.Tx[db.Tenant]) (*gorm.DB, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("crud: the caller ended before the statement: %w", context.Cause(ctx))
	}
	return tx.DB(), nil
}

// Reset clears the metadata the server owns, whatever a caller sent for it.
// A create route calls it on the body it decoded, so a caller can neither choose
// an id nor backdate a row; the Entity constraint retains the embedded Base.
func Reset[T Entity](e T) { *entity.BaseOf(e) = Base{} }

// blank is a new zero entity. T is a pointer type, so new(T) would be a pointer
// to a pointer; this is the one place the package needs reflection to say
// "another one of those".
func blank[T Entity]() T {
	var zero T
	return reflect.New(reflect.TypeOf(zero).Elem()).Interface().(T)
}

// isNil reports whether e is a nil entity. T is a pointer type, so this is
// reachable: a request with no body at all decodes to one, and every method
// below dereferences. e == nil does not compile for a type parameter and
// any(e) == nil is false for a typed nil, so it is reflection or nothing.
func isNil[T Entity](e T) bool {
	v := reflect.ValueOf(e)
	return !v.IsValid() || v.IsNil()
}

func validate(ctx context.Context, e any) error {
	if err := entity.ValidateSketchFields(e); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	v, ok := e.(Validator)
	if !ok {
		return nil
	}
	if err := v.Validate(ctx); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	return nil
}

// ordering turns "field" or "-field" into an ORDER BY, always ending in the id
// so the order is total.
func ordering(fields []Field, sort string) (string, error) {
	if sort == "" {
		return "created_at DESC, id", nil
	}
	name, dir := sort, "ASC"
	if name[0] == '-' {
		name, dir = name[1:], "DESC"
	}
	f, err := comparable(fields, name, "sort")
	if err != nil {
		return "", err
	}
	return f.Column + " " + dir + ", id", nil
}

// comparable is the field with this name, refused when a query cannot compare
// against it. A list column holds many values, so "roles = ?" is not a question
// about any of them and ORDER BY roles is an order nobody asked for; a
// containment filter is a different operator and it does not exist yet. Saying
// so is better than the silence a name outside the schema used to get, which
// was the accident that kept roles unpatchable.
func comparable(fields []Field, name, what string) (Field, error) {
	f, ok := FieldNamed(fields, name)
	switch {
	case !ok:
		return Field{}, fmt.Errorf("%w: there is no field %q to %s on", ErrInvalid, name, what)
	case f.Type == entity.TypeSketch:
		return Field{}, fmt.Errorf("%w: %s is a sketch and cannot be used to %s", ErrInvalid, name, what)
	case f.Type == entity.TypePicture:
		return Field{}, fmt.Errorf("%w: %s is a derived picture, which is not something to %s on", ErrInvalid, name, what)
	case f.Type == TypeList:
		return Field{}, fmt.Errorf("%w: %s is a list, which is not something to %s on", ErrInvalid, name, what)
	}
	return f, nil
}

// Classify names the two database failures a caller can do something about: a
// row that is not there, and a unique constraint the write contradicts.
// Everything else is ours and comes back unchanged, to be logged and answered
// with a 500. One failure that is not ours arrives through here too: a value of a
// kind this package admits that the column's own type cannot hold, which the driver
// refuses on the way to the wire.
//
// It is exported because a module that writes rows this package cannot still
// has to answer with the same errors. A tenant carries no tenant_id, so it is
// not an Entity and modules/tenant writes it by hand; it used to carry a copy
// of this function, which was a second opinion about what a 409 means waiting
// to drift.
func Classify(err error) error {
	pg, isPostgres := errors.AsType[*pgconn.PgError](err)
	switch {
	case errors.Is(err, entity.ErrInvalidSketch):
		return fmt.Errorf("%w: %w", ErrInvalid, err)
	case errors.Is(err, gorm.ErrRecordNotFound):
		return ErrNotFound
	case isPostgres && pg.Code == "23505":
		return &UniqueConflict{Constraint: pg.ConstraintName}
	}
	return err
}
