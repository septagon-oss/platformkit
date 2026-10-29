package crud_test

// The two doors name the two answers the bool in Delete(tx, id, soft) carried, and a
// rename must not change which rows either touches, the stamp the hiding one writes, or
// the permanence the removing one promises: a soft DeleteRow turns a purge into a miss.

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

// ddlOwned is one row that points at a task, so removal is the database's answer.
const ddlOwned = `
CREATE TABLE crud_task_owners (
	id uuid PRIMARY KEY,
	tenant_id uuid NOT NULL,
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now(),
	task uuid NOT NULL REFERENCES crud_tasks (id)
);
ALTER TABLE crud_task_owners ENABLE ROW LEVEL SECURITY;
ALTER TABLE crud_task_owners FORCE ROW LEVEL SECURITY;
CREATE POLICY crud_task_owners_tenant ON crud_task_owners
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));`

// setupOwned gives the test the task table and one table that references it.
func setupOwned(t *testing.T) *db.Conn {
	t.Helper()
	admin, app := dbtest.Schema(t)
	if _, err := admin.ExecContext(t.Context(), ddl+ddlOwned); err != nil {
		t.Fatalf("create the fixtures: %v", err)
	}
	return app
}

// write returns one task of acme by id.
func write(t *testing.T, conn *db.Conn, title string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		task := &Task{Title: title}
		if err := crud.Create(ctx, tx, task); err != nil {
			t.Fatalf("Create: %v", err)
		}
		id = task.ID
	})
	return id
}

// deletedAt reads back the stamp a row carries, as its tenant's transaction sees it.
func deletedAt(t *testing.T, tx db.Tx[db.Tenant], id uuid.UUID) (time.Time, bool) {
	t.Helper()
	var stamp sql.NullTime
	if err := tx.DB().Raw("SELECT deleted_at FROM crud_tasks WHERE id = ?", id).Scan(&stamp).Error; err != nil {
		t.Fatalf("read the stamp back: %v", err)
	}
	if !stamp.Valid {
		return time.Time{}, false
	}
	return stamp.Time, true
}

// TestSoftDeleteStampsTheRowDeleteWithSoftTrueStamped: the row stays in the table,
// hidden from every read, stamped with the instant the column keeps — and a row already
// hidden is refused.
func TestSoftDeleteStampsTheRowDeleteWithSoftTrueStamped(t *testing.T) {
	conn := setupOwned(t)
	hidden, alsoHidden := write(t, conn, "hidden by the new door"), write(t, conn, "hidden by the old one")
	started := time.Now().Add(-time.Second)

	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		if err := crud.SoftDelete[*Task](ctx, tx, hidden); err != nil {
			t.Fatalf("SoftDelete: %v", err)
		}
		if err := crud.Delete[*Task](tx, alsoHidden, true); err != nil {
			t.Fatalf("Delete(…, true): %v", err)
		}
		for _, id := range []uuid.UUID{hidden, alsoHidden} {
			stamp, held := deletedAt(t, tx, id)
			if !held {
				t.Errorf("%s left the table or lost its stamp; a soft delete keeps the row", id)
				continue
			}
			if stamp.Before(started) || stamp.After(time.Now().Add(time.Second)) {
				t.Errorf("%s carries the stamp %s, which is not the instant the delete happened", id, stamp)
			}
			if _, err := crud.Get[*Task](tx, id); !errors.Is(err, crud.ErrNotFound) {
				t.Errorf("Get found a row both doors just hid: %v", err)
			}
			if _, err := crud.GetCtx[*Task](ctx, tx, id); !errors.Is(err, crud.ErrNotFound) {
				t.Errorf("GetCtx found a row both doors just hid: %v", err)
			}
		}
		if total, err := crud.CountCtx[*Task](ctx, tx); err != nil || total != 0 {
			t.Errorf("the tenant counts %d live rows, %v, want none", total, err)
		}
		if rows, total, err := crud.ListCtx[*Task](ctx, tx, crud.Query{}); err != nil || len(rows) != 0 || total != 0 {
			t.Errorf("ListCtx returned %d of %d rows, %v, want a hidden pair", len(rows), total, err)
		}
		if err := crud.SoftDelete[*Task](ctx, tx, hidden); !errors.Is(err, crud.ErrNotFound) {
			t.Errorf("hiding the same row twice answered %v, want ErrNotFound and no second write", err)
		}
	})

	// Another tenant's row is no row to hide, and the row it points at stays live.
	theirs := write(t, conn, "still someone else's to hide")
	as(t, conn, globex, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		if err := crud.SoftDelete[*Task](ctx, tx, theirs); !errors.Is(err, crud.ErrNotFound) {
			t.Errorf("a wrong-tenant SoftDelete answered %v, want ErrNotFound", err)
		}
	})
	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		if _, held := deletedAt(t, tx, theirs); held {
			t.Error("a refused wrong-tenant delete still stamped the row")
		}
	})
}

// TestDeleteRowRemovesTheRowDeleteWithSoftFalseRemoved: both doors take the row out of the table, not hide it.
func TestDeleteRowRemovesTheRowDeleteWithSoftFalseRemoved(t *testing.T) {
	conn := setupOwned(t)
	gone, alsoGone := write(t, conn, "removed by the new door"), write(t, conn, "removed by the old one")

	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		if err := crud.DeleteRow[*Task](ctx, tx, gone); err != nil {
			t.Fatalf("DeleteRow: %v", err)
		}
		if err := crud.Delete[*Task](tx, alsoGone, false); err != nil {
			t.Fatalf("Delete(…, false): %v", err)
		}
		for _, id := range []uuid.UUID{gone, alsoGone} {
			var held bool
			if err := tx.DB().Raw("SELECT EXISTS (SELECT 1 FROM crud_tasks WHERE id = ?)", id).Scan(&held).Error; err != nil {
				t.Fatal(err)
			}
			if held {
				t.Errorf("%s is still in the table; DeleteRow is the hard delete Delete(…, false) was", id)
			}
			if _, err := crud.GetCtx[*Task](ctx, tx, id); !errors.Is(err, crud.ErrNotFound) {
				t.Errorf("GetCtx found a removed row: %v", err)
			}
		}
		if total, err := crud.CountCtx[*Task](ctx, tx); err != nil || total != 0 {
			t.Errorf("the tenant counts %d rows, %v, want none", total, err)
		}
		before, err := crud.CountCtx[*Task](ctx, tx)
		if err != nil {
			t.Fatal(err)
		}
		if err := crud.DeleteRow[*Task](ctx, tx, uuid.New()); !errors.Is(err, crud.ErrNotFound) {
			t.Errorf("removing a row that is not there answered %v, want ErrNotFound", err)
		}
		if after, err := crud.CountCtx[*Task](ctx, tx); err != nil || after != before {
			t.Errorf("the refusal changed the count, %d against %d (%v)", after, before, err)
		}
	})

	// A row a foreign key points at is the database's refusal: both doors hand back the
	// same verdict, nothing removed, the referring row kept.
	referenced := write(t, conn, "someone still points at me")
	var newText, oldText string
	attempt(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		attach(t, tx, referenced)
		newText = errText(crud.DeleteRow[*Task](ctx, tx, referenced))
	})
	attempt(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		attach(t, tx, referenced)
		oldText = errText(crud.Delete[*Task](tx, referenced, false))
	})
	if newText == "" || newText != oldText {
		t.Errorf("the two doors refused a referenced row differently: %q against %q", newText, oldText)
	}
	if errors.Is(errors.New(newText), crud.ErrNotFound) {
		t.Error("a referenced row was answered as a row that is not there")
	}
}

// attach points one owner row of the same tenant at task, in the delete's transaction.
func attach(t *testing.T, tx db.Tx[db.Tenant], task uuid.UUID) {
	t.Helper()
	if err := tx.DB().Exec(`INSERT INTO crud_task_owners (id, tenant_id, task)
		SELECT gen_random_uuid(), ?, ?`, db.TenantOf(tx).ID, task).Error; err != nil {
		t.Fatalf("attach a referring row: %v", err)
	}
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
