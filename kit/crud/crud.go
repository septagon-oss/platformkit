// Package crud is the five operations every tenant-owned resource needs.
// Entity definitions and schemas are owned by kit/entity, with aliases here
// for existing callers.
//
// A module writes a struct with an embedded Base and gets read, list, create,
// update and delete, each refusing what row-level security would refuse anyway.
// What it does not write is a repository, a DTO or a mapper.
//
// # This half links no web server
//
// Existing module contracts may import this package for Base. A caller that
// needs only entity definitions or field metadata can instead import kit/entity
// without linking a database driver. HTTP routes, PATCH merging and OpenAPI
// declarations belong to kit/rest, which imports this storage adapter.
//
// # Instantiate with the pointer type
//
// Entity is implemented by *Task and not by Task: base() has a pointer
// receiver, because the kernel stamps the tenant into it. So every function
// here is instantiated with the pointer — crud.Get[*Task](tx, id) — and List
// returns []*Task. The alternative, two type parameters everywhere, costs every
// call site a repetition the compiler could not check anyway.
//
// # The tenant is never a parameter
//
// Create takes the tenant from the transaction, and Update refuses an entity
// carrying a different one. That is defense in depth rather than the boundary:
// row-level security refuses the same write in the database, whatever Go
// believes. See docs/adr/0003.
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
)

// The three failures a caller distinguishes. Everything else is an outage and
// reads as a 500. Spec.Mount turns these into 404, 422 and 409.
var (
	// ErrNotFound is no such row in this tenant. Another tenant's row is not
	// found either, which is the only thing the API may say about it.
	ErrNotFound = errors.New("crud: no such row")
	// ErrInvalid is the entity's own Validate, or a query naming a field that
	// does not exist.
	ErrInvalid = errors.New("crud: invalid")
	// ErrConflict is a write that contradicts existing data or business state.
	ErrConflict = errors.New("crud: conflict")
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

// Query is a list request: a page, an order and a set of equality filters.
// Sort is "field" or "-field" and every name is checked against the entity's
// schema, so a column name never comes from a caller.
type Query struct {
	Limit, Offset int
	Sort          string
	Filter        map[string]any
}

// The page bounds. A caller that asks for nothing gets a screenful; a caller
// that asks for everything gets the most a single response should carry.
const (
	DefaultLimit = 50
	MaxLimit     = 200
)

// Get reads one row of this tenant. A soft-deleted row is not found.
func Get[T Entity](tx db.Tx[db.Tenant], id uuid.UUID) (T, error) {
	return get[T](tx.DB(), id)
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
func Count[T Entity](tx db.Tx[db.Tenant]) (int64, error) {
	var total int64
	err := tx.DB().Model(blank[T]()).Where("deleted_at IS NULL").Count(&total).Error
	return total, Classify(err)
}

// List reads a page of this tenant's rows and the total the page came from.
// The order always ends in the id, so two pages of equal-keyed rows do not
// overlap or skip.
func List[T Entity](tx db.Tx[db.Tenant], q Query) ([]T, int64, error) {
	fields := Fields[T]()
	where, args, err := conditions(fields, q.Filter)
	if err != nil {
		return nil, 0, err
	}
	order, err := ordering(fields, q.Sort)
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
		g := tx.DB().Model(blank[T]()).Where("deleted_at IS NULL")
		for i, w := range where {
			g = g.Where(w, args[i])
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

// RecheckTenant is the tenant-scope recheck Update performs on the row before it
// writes it: an entity carrying a tenant that is not the transaction's is not
// this caller's row, and is answered ErrNotFound, the only thing the API may say
// about it. A row carrying no tenant is the transaction's to stamp, which is what
// Update then does with it. Nothing at all is answered ErrInvalid, the answer
// Create and Update give for nothing to act on: the non-nil precondition cannot
// be a sentence in the doc of an exported door, because the caller the export
// exists for has no Update above it to pass the guard first.
//
// It is exported for a caller that read a row under the lock and then decided to
// write nothing, which is where row-level security stops being the backstop. On a
// table whose read policy shows one shared list to every tenant — the catalogue
// shape of docs/adr/0008 — GetForUpdate answers a row the request may read and
// may not write, and a write that never happens is one WITH CHECK never gets to
// refuse. A patch body that names no column is that write, and the answer it gets
// has to be the answer the same row gives a body that names a column.
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

// Update writes an existing row back. It refuses an entity carrying another
// tenant, which row-level security would refuse too; reporting it as not found
// is the same answer the read would have given.
//
// columns names the database columns to write, and no columns means all of
// them. The distinction is what makes two concurrent PATCHes of different
// fields both survive: writing every column means the second request writes the
// first one's fields back to what they were when it read them, so a change to a
// field nobody touched is lost. Spec.Mount passes exactly the columns the patch
// body named. crud.Schema is the only thing that produces these names; a caller
// that invents one gets whatever GORM makes of it.
//
// Read with GetForUpdate before merging or validating stored state; acquiring
// a lock only at this write cannot correct an earlier decision on a stale row.
//
// A failed write aborts the whole transaction, in Postgres as everywhere: a
// caller that means to try something else after a conflict needs a new
// transaction, which for an HTTP handler means a new request.
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
func Delete[T Entity](tx db.Tx[db.Tenant], id uuid.UUID, soft bool) error {
	e := blank[T]()
	res := tx.DB().Model(e).Where("id = ? AND deleted_at IS NULL", id)
	if soft {
		// db.Now, because the stamp has to be the instant the column keeps.
		res = res.Update("deleted_at", db.Now())
	} else {
		res = res.Delete(e)
	}
	if res.Error != nil {
		return Classify(res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
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
	v, ok := e.(Validator)
	if !ok {
		return nil
	}
	if err := v.Validate(ctx); err != nil {
		return fmt.Errorf("%w: %s", ErrInvalid, err)
	}
	return nil
}

// conditions turns a filter map into predicates, one per field, with the column
// taken from the schema. A name the schema does not know is refused rather than
// ignored: a filter that silently does nothing returns the wrong page.
func conditions(fields []Field, filter map[string]any) ([]string, []any, error) {
	where := make([]string, 0, len(filter))
	args := make([]any, 0, len(filter))
	for name, v := range filter {
		f, err := comparable(fields, name, "filter")
		if err != nil {
			return nil, nil, err
		}
		where = append(where, f.Column+" = ?")
		args = append(args, v)
	}
	return where, args, nil
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
	case f.Type == TypeList:
		return Field{}, fmt.Errorf("%w: %s is a list, which is not something to %s on", ErrInvalid, name, what)
	}
	return f, nil
}

// Classify names the two database failures a caller can do something about: a
// row that is not there, and a unique constraint the write contradicts.
// Everything else is ours and comes back unchanged, to be logged and answered
// with a 500.
//
// It is exported because a module that writes rows this package cannot still
// has to answer with the same errors. A tenant carries no tenant_id, so it is
// not an Entity and modules/tenant writes it by hand; it used to carry a copy
// of this function, which was a second opinion about what a 409 means waiting
// to drift.
func Classify(err error) error {
	pg, isPostgres := errors.AsType[*pgconn.PgError](err)
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return ErrNotFound
	case isPostgres && pg.Code == "23505":
		return &UniqueConflict{Constraint: pg.ConstraintName}
	}
	return err
}
