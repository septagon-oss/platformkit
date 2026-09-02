package crud_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// Task is a module's entity, written the way a module writes one: a struct, an
// embedded Base, a table name and nothing else. Every tag the schema derivation
// reads appears somewhere below.
type Task struct {
	crud.Base
	Title    string     `json:"title" validate:"required" ui:"widget:text"`
	Status   string     `json:"status,omitempty" enum:"open,done" ui:"widget:select"`
	Priority int        `json:"priority,omitempty"`
	Done     bool       `json:"done,omitempty" ui:"hide:list"`
	Notes    string     `json:"notes,omitempty" gorm:"type:text"`
	DueAt    *time.Time `json:"dueAt,omitempty"`
	Secret   string     `json:"-" gorm:"-"`
}

func (Task) TableName() string { return "crud_tasks" }

// Validate is the optional check. An empty title is 422, not 500.
func (t *Task) Validate(context.Context) error {
	if strings.TrimSpace(t.Title) == "" {
		return errors.New("a task needs a title")
	}
	return nil
}

const ddl = `
CREATE TABLE crud_tasks (
	id uuid PRIMARY KEY,
	tenant_id uuid NOT NULL,
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now(),
	deleted_at timestamptz,
	title text NOT NULL,
	status text NOT NULL DEFAULT 'open',
	priority int NOT NULL DEFAULT 0,
	done boolean NOT NULL DEFAULT false,
	notes text NOT NULL DEFAULT '',
	due_at timestamptz,
	UNIQUE (tenant_id, title)
);
ALTER TABLE crud_tasks ENABLE ROW LEVEL SECURITY;
ALTER TABLE crud_tasks FORCE ROW LEVEL SECURITY;
CREATE POLICY crud_tasks_tenant ON crud_tasks
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));`

var (
	acme   = tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}
	globex = tenancy.Tenant{ID: uuid.New(), Slug: "globex", Name: "Globex"}
)

// setup gives the test the tasks table and the application connection.
func setup(t *testing.T) *db.Conn {
	t.Helper()
	admin, app := dbtest.Schema(t)
	if _, err := admin.ExecContext(t.Context(), ddl); err != nil {
		t.Fatalf("create tasks: %v", err)
	}
	return app
}

// as runs fn in one tenant's transaction, failing the test if it does not
// commit. It is the shape every caller of kit/crud has.
func as(t *testing.T, conn *db.Conn, tenant tenancy.Tenant, fn func(ctx context.Context, tx db.Tx[db.Tenant])) {
	t.Helper()
	err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		fn(ctx, tx)
		return nil
	})
	if err != nil {
		t.Fatalf("transaction as %s: %v", tenant.Slug, err)
	}
}

// attempt is as for an operation that is meant to fail. A statement Postgres
// refuses aborts the transaction it was in — a refusal is not a return value
// there — so anything after it in the same transaction would fail for the wrong
// reason, and the transaction itself cannot commit. One attempt, one
// transaction, and the rollback is the expected end.
func attempt(t *testing.T, conn *db.Conn, tenant tenancy.Tenant, fn func(ctx context.Context, tx db.Tx[db.Tenant])) {
	t.Helper()
	_ = db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		fn(ctx, tx)
		return errors.New("rolled back on purpose")
	})
}

// TestCreateStampsTheTenantFromTheTransaction: a module never assigns a tenant,
// and a module that tries is overruled.
func TestCreateStampsTheTenantFromTheTransaction(t *testing.T) {
	conn := setup(t)
	var id uuid.UUID
	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		task := &Task{Title: "write the kernel"}
		task.TenantID = globex.ID // a module's mistake, or worse
		if err := crud.Create(ctx, tx, task); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if task.TenantID != acme.ID {
			t.Errorf("TenantID = %s, want the transaction's %s", task.TenantID, acme.ID)
		}
		if task.ID == uuid.Nil || task.CreatedAt.IsZero() {
			t.Errorf("Create left the id or the timestamps unset: %+v", task.Base)
		}
		id = task.ID
	})
	as(t, conn, acme, func(_ context.Context, tx db.Tx[db.Tenant]) {
		got, err := crud.Get[*Task](tx, id)
		if err != nil || got.Title != "write the kernel" {
			t.Fatalf("Get = %v, %v", got, err)
		}
	})
}

// TestAnotherTenantReachesNothing is the isolation claim at the level a module
// works at: not one of the five operations sees the other tenant's row, and it
// is Postgres that refuses, not a predicate anyone remembered to write.
func TestAnotherTenantReachesNothing(t *testing.T) {
	conn := setup(t)
	mine := &Task{Title: "acme only"}
	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		if err := crud.Create(ctx, tx, mine); err != nil {
			t.Fatalf("Create: %v", err)
		}
	})

	as(t, conn, globex, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		if _, err := crud.Get[*Task](tx, mine.ID); !errors.Is(err, crud.ErrNotFound) {
			t.Errorf("Get across tenants = %v, want ErrNotFound", err)
		}
		items, total, err := crud.List[*Task](tx, crud.Query{})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(items) != 0 || total != 0 {
			t.Errorf("List across tenants = %d items, total %d, want none", len(items), total)
		}
	})
	// An update of a row this tenant cannot see writes nothing: the policy's
	// USING clause removes it from the UPDATE's own scan.
	attempt(t, conn, globex, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		stolen := &Task{Title: "mine now"}
		stolen.ID, stolen.TenantID = mine.ID, globex.ID
		if err := crud.Update(ctx, tx, stolen); !errors.Is(err, crud.ErrNotFound) {
			t.Errorf("Update across tenants = %v, want ErrNotFound", err)
		}
	})
	attempt(t, conn, globex, func(_ context.Context, tx db.Tx[db.Tenant]) {
		if err := crud.Delete[*Task](tx, mine.ID, false); !errors.Is(err, crud.ErrNotFound) {
			t.Errorf("Delete across tenants = %v, want ErrNotFound", err)
		}
	})

	// And the row is still there, untouched.
	as(t, conn, acme, func(_ context.Context, tx db.Tx[db.Tenant]) {
		got, err := crud.Get[*Task](tx, mine.ID)
		if err != nil || got.Title != "acme only" {
			t.Errorf("the row did not survive the other tenant: %v, %v", got, err)
		}
	})
}

// TestUpdateRefusesAnEntityFromAnotherTenant is the Go half of the same claim:
// the type says Tx[Tenant] and the kernel still checks, because defense in
// depth is what "the database is the boundary" is allowed to mean.
func TestUpdateRefusesAnEntityFromAnotherTenant(t *testing.T) {
	conn := setup(t)
	task := &Task{Title: "ours"}
	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		if err := crud.Create(ctx, tx, task); err != nil {
			t.Fatalf("Create: %v", err)
		}
	})
	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		task.TenantID = globex.ID
		// Refused in Go, before the statement: this one never reaches Postgres,
		// so the transaction is still usable afterwards.
		if err := crud.Update(ctx, tx, task); !errors.Is(err, crud.ErrNotFound) {
			t.Errorf("Update with a foreign tenant = %v, want ErrNotFound", err)
		}
	})
}

// TestValidateRefusesTheWrite: an entity that says no is not written.
func TestValidateRefusesTheWrite(t *testing.T) {
	conn := setup(t)
	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		if err := crud.Create(ctx, tx, &Task{Title: "  "}); !errors.Is(err, crud.ErrInvalid) {
			t.Fatalf("Create of an invalid task = %v, want ErrInvalid", err)
		}
		_, total, err := crud.List[*Task](tx, crud.Query{})
		if err != nil || total != 0 {
			t.Errorf("the refused task was written anyway: %d rows, %v", total, err)
		}
	})
}

// TestUniqueViolationIsAConflict, and not a 500.
func TestUniqueViolationIsAConflict(t *testing.T) {
	conn := setup(t)
	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		if err := crud.Create(ctx, tx, &Task{Title: "once"}); err != nil {
			t.Fatalf("Create: %v", err)
		}
	})
	attempt(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		err := crud.Create(ctx, tx, &Task{Title: "once"})
		if !errors.Is(err, crud.ErrConflict) {
			t.Errorf("Create of a duplicate = %v, want ErrConflict", err)
		}
	})
}

// TestSoftDeleteHidesTheRowFromGetAndList, while it stays in the table for
// whatever still points at it.
func TestSoftDeleteHidesTheRowFromGetAndList(t *testing.T) {
	conn := setup(t)
	task := &Task{Title: "temporary"}
	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		if err := crud.Create(ctx, tx, task); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if err := crud.Delete[*Task](tx, task.ID, true); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		if _, err := crud.Get[*Task](tx, task.ID); !errors.Is(err, crud.ErrNotFound) {
			t.Errorf("Get of a soft-deleted row = %v, want ErrNotFound", err)
		}
		items, total, err := crud.List[*Task](tx, crud.Query{})
		if err != nil || len(items) != 0 || total != 0 {
			t.Errorf("List showed a soft-deleted row: %d items, total %d, %v", len(items), total, err)
		}
		// Deleting it again finds nothing to delete, rather than reporting
		// success twice.
		if err := crud.Delete[*Task](tx, task.ID, true); !errors.Is(err, crud.ErrNotFound) {
			t.Errorf("a second soft delete = %v, want ErrNotFound", err)
		}
	})

	var rows int
	as(t, conn, acme, func(_ context.Context, tx db.Tx[db.Tenant]) {
		if err := tx.DB().Raw("SELECT count(*) FROM crud_tasks WHERE deleted_at IS NOT NULL").Scan(&rows).Error; err != nil {
			t.Fatalf("count: %v", err)
		}
	})
	if rows != 1 {
		t.Errorf("%d soft-deleted rows in the table, want 1", rows)
	}
}

// TestListPagesSortsAndFilters, and refuses a field the schema does not have:
// the column names in the SQL come from the struct, never from the caller.
func TestListPagesSortsAndFilters(t *testing.T) {
	conn := setup(t)
	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		for i, title := range []string{"a", "b", "c"} {
			task := &Task{Title: title, Priority: i, Status: "open"}
			if i == 2 {
				task.Status = "done"
			}
			if err := crud.Create(ctx, tx, task); err != nil {
				t.Fatalf("Create %s: %v", title, err)
			}
		}
	})
	as(t, conn, acme, func(_ context.Context, tx db.Tx[db.Tenant]) {
		items, total, err := crud.List[*Task](tx, crud.Query{Sort: "-priority", Limit: 2})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if total != 3 || len(items) != 2 {
			t.Fatalf("List = %d of %d, want 2 of 3", len(items), total)
		}
		if items[0].Title != "c" || items[1].Title != "b" {
			t.Errorf("descending by priority gave %s, %s", items[0].Title, items[1].Title)
		}

		items, total, err = crud.List[*Task](tx, crud.Query{Filter: map[string]any{"status": "done"}})
		if err != nil || total != 1 || items[0].Title != "c" {
			t.Errorf("filtered list = %v, total %d, %v", items, total, err)
		}

		for _, q := range []crud.Query{
			{Sort: "tenantId"},                           // json:"-": not a field out here
			{Sort: "-nonesuch"},                          //
			{Filter: map[string]any{"title; DROP": "x"}}, // and nothing to interpolate
		} {
			if _, _, err := crud.List[*Task](tx, q); !errors.Is(err, crud.ErrInvalid) {
				t.Errorf("List(%+v) = %v, want ErrInvalid", q, err)
			}
		}
	})
}

// TestSchemaIsDerivedFromTheStruct: every tag a module can write, read back.
func TestSchemaIsDerivedFromTheStruct(t *testing.T) {
	spec := crud.Spec[*Task]{Module: "tasks", Entity: "task", Path: "/api/tasks",
		Read: "task:read", Write: "task:write"}
	schema := spec.Schema()
	byName := map[string]crud.Field{}
	for _, f := range schema.Fields {
		byName[f.Name] = f
	}

	for _, hidden := range []string{"-", "tenantId", "deletedAt", "Secret"} {
		if _, ok := byName[hidden]; ok {
			t.Errorf("the schema exposes %q", hidden)
		}
	}
	for _, want := range []crud.Field{
		{Name: "id", Type: crud.TypeUUID, ReadOnly: true},
		{Name: "createdAt", Type: crud.TypeTime, ReadOnly: true},
		{Name: "updatedAt", Type: crud.TypeTime, ReadOnly: true},
		{Name: "title", Type: crud.TypeString, Required: true, Widget: "text"},
		{Name: "status", Type: crud.TypeString, Widget: "select", Enum: []string{"open", "done"}},
		{Name: "priority", Type: crud.TypeInt},
		{Name: "done", Type: crud.TypeBool, HideList: true},
		{Name: "notes", Type: crud.TypeText},
		{Name: "dueAt", Type: crud.TypeTime},
	} {
		got, ok := byName[want.Name]
		if !ok {
			t.Errorf("the schema has no field %q", want.Name)
			continue
		}
		if got.Type != want.Type || got.Widget != want.Widget || got.Required != want.Required ||
			got.ReadOnly != want.ReadOnly || got.HideList != want.HideList ||
			strings.Join(got.Enum, ",") != strings.Join(want.Enum, ",") {
			t.Errorf("field %s = %+v, want %+v", want.Name, got, want)
		}
	}
	if schema.Module != "tasks" || schema.Entity != "task" || schema.Path != "/api/tasks" {
		t.Errorf("schema = %+v, want the Spec's own names", schema)
	}
}

// TestSpecRefusesToMountNonsense: a Spec that could only produce broken routes
// fails at the mount site, like every other wiring mistake in this kernel.
func TestSpecRefusesToMountNonsense(t *testing.T) {
	for name, spec := range map[string]crud.Spec[*Task]{
		"no path":        {Module: "tasks", Entity: "task", Path: "api", Read: "task:read", Write: "task:write"},
		"no event name":  {Module: "Tasks", Entity: "task", Path: "/api", Read: "task:read", Write: "task:write"},
		"bad permission": {Module: "tasks", Entity: "task", Path: "/api", Read: "read", Write: "task:write"},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("Mount accepted it")
				}
			}()
			spec.Mount(&httpx.API{})
		})
	}
}
