// Package crud is the five operations every tenant-owned resource needs, and
// the schema a generated screen reads.
//
// A module declares one Spec per entity and gets list, create, read, update and
// delete as declared routes, each with its permission, each emitting the events
// the manifest promises. What a module writes is the struct and the Spec; what
// it does not write is a repository, a service, a handler, a DTO or a mapper.
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
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	"github.com/septagon-oss/platformkit/kit/db"
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
	// ErrConflict is a unique constraint the write contradicts.
	ErrConflict = errors.New("crud: conflict")
)

// Base is embedded by every tenant-owned entity. The kernel sets TenantID from
// the transaction's scope; a module never assigns it, and never sees it in
// JSON either, because a tenant that a caller could send is a tenant a caller
// could change.
//
// The three fields a caller may read are marked read-only for OpenAPI and not
// required in a request body: the server sets all of them, so a create that had
// to send an id would be a create that could choose one.
type Base struct {
	ID        uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id" required:"false" readOnly:"true"`
	TenantID  uuid.UUID  `gorm:"type:uuid;not null" json:"-"`
	CreatedAt time.Time  `json:"createdAt" required:"false" readOnly:"true"`
	UpdatedAt time.Time  `json:"updatedAt" required:"false" readOnly:"true"`
	DeletedAt *time.Time `gorm:"index" json:"-"`
}

// base is how the kernel reaches the embedded fields of any entity. It is
// unexported, so the Entity interface is closed to types that embed Base: a
// struct cannot claim to be an entity without carrying the tenant column that
// makes it one.
func (b *Base) base() *Base { return b }

// Entity is a tenant-owned row: a table name and an embedded Base.
type Entity interface {
	TableName() string
	base() *Base
}

// Validator is the optional check an entity makes of itself before it is
// written. It takes a context and no transaction on purpose: a validation that
// queries is either a database constraint (which answers with ErrConflict) or a
// Spec hook, not a rule hidden inside a setter.
type Validator interface {
	Validate(ctx context.Context) error
}

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
	e := blank[T]()
	err := tx.DB().Where("id = ? AND deleted_at IS NULL", id).Take(e).Error
	if err != nil {
		var zero T
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return zero, ErrNotFound
		}
		return zero, classify(err)
	}
	return e, nil
}

// List reads a page of this tenant's rows and the total the page came from.
// The order always ends in the id, so two pages of equal-keyed rows do not
// overlap or skip.
func List[T Entity](tx db.Tx[db.Tenant], q Query) ([]T, int64, error) {
	fields := fieldsOf[T]()
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
		return nil, 0, classify(err)
	}
	var out []T
	if err := build().Order(order).Limit(limit).Offset(max(q.Offset, 0)).Find(&out).Error; err != nil {
		return nil, 0, classify(err)
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
	b := e.base()
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
		return classify(err)
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
// A failed write aborts the whole transaction, in Postgres as everywhere: a
// caller that means to try something else after a conflict needs a new
// transaction, which for an HTTP handler means a new request.
func Update[T Entity](ctx context.Context, tx db.Tx[db.Tenant], e T, columns ...string) error {
	if isNil(e) {
		return fmt.Errorf("%w: there is nothing to update", ErrInvalid)
	}
	b := e.base()
	if b.ID == uuid.Nil {
		return ErrNotFound
	}
	tenant := db.TenantOf(tx).ID
	if b.TenantID != uuid.Nil && b.TenantID != tenant {
		return ErrNotFound
	}
	b.TenantID = tenant
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
		return classify(res.Error)
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
		res = res.Update("deleted_at", time.Now())
	} else {
		res = res.Delete(e)
	}
	if res.Error != nil {
		return classify(res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

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
		f, ok := fieldNamed(fields, name)
		if !ok {
			return nil, nil, fmt.Errorf("%w: there is no field %q to filter on", ErrInvalid, name)
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
	f, ok := fieldNamed(fields, name)
	if !ok {
		return "", fmt.Errorf("%w: there is no field %q to sort on", ErrInvalid, sort)
	}
	return f.Column + " " + dir + ", id", nil
}

func fieldNamed(fields []Field, name string) (Field, bool) {
	for _, f := range fields {
		if f.Name == name {
			return f, true
		}
	}
	return Field{}, false
}

// classify names the one database failure a caller can do something about. A
// unique violation is the caller's problem; everything else is ours.
func classify(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		return fmt.Errorf("%w: %s", ErrConflict, pg.ConstraintName)
	}
	return err
}
