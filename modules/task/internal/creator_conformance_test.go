package internal_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/task"
	"github.com/septagon-oss/platformkit/modules/task/contracts"
	"github.com/septagon-oss/platformkit/modules/task/contracts/tasktest"
	"github.com/septagon-oss/platformkit/modules/task/internal"
)

// TestCreatorConforms runs the creator suite against the real service in a real
// tenant transaction, including the cases only a transaction can show: a refused
// create and a failing outbox insert each take the caller's own write back.
func TestCreatorConforms(t *testing.T) {
	tasktest.RunCreator(t, func(t *testing.T, run func(tasktest.Fixture)) {
		admin, conn := dbtest.Schema(t, task.Migrations)
		svc := internal.NewService()
		svc.Policy = tasktest.Policy{}
		mustExec(t, admin, `CREATE TABLE callers_rows (id uuid PRIMARY KEY, tenant_id uuid NOT NULL)`)
		mustExec(t, admin, `GRANT SELECT, INSERT ON callers_rows TO CURRENT_USER`)
		refuseNamedEvent(t, admin)
		err := db.Run(tasktest.As(tenancy.WithTenant(t.Context(), acme), tasktest.Holder), conn,
			func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				run(tasktest.Fixture{
					Ctx: ctx, Tx: tx, Service: svc, Creator: svc, Reader: svc,
					Seed: func(row *contracts.Task) uuid.UUID {
						if err := crud.Create(ctx, tx, row); err != nil {
							t.Fatalf("seed a task: %v", err)
						}
						return row.ID
					},
					Task: func(id uuid.UUID) (contracts.Task, error) {
						got, err := crud.Get[*contracts.Task](tx, id)
						if err != nil {
							return contracts.Task{}, err
						}
						return *got, nil
					},
					Published: func() []string { return outboxNames(t, tx) },
					Tasks:     func() int { return count(t, tx, "tasks") },
					Atomic:    atomic(t, admin, tx),
				})
				return errRollback
			})
		if !errors.Is(err, errRollback) {
			t.Fatalf("the case's transaction: %v", err)
		}
	})
}

func mustExec(t *testing.T, admin *sql.DB, query string) {
	t.Helper()
	if _, err := admin.ExecContext(t.Context(), query); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}

func outboxNames(t *testing.T, tx db.Tx[db.Tenant]) []string {
	t.Helper()
	var names []string
	if err := tx.DB().Raw(`SELECT name FROM `+outbox+` WHERE tenant_id = ? ORDER BY created_at, id`, acme.ID).Scan(&names).Error; err != nil {
		t.Fatalf("read the outbox: %v", err)
	}
	return names
}

func count(t *testing.T, tx db.Tx[db.Tenant], table string) int {
	t.Helper()
	var n int
	if err := tx.DB().Raw(`SELECT count(*) FROM ` + table).Scan(&n).Error; err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// atomic is the transaction-shaped half of the fixture: Attempt is a savepoint
// inside the case's transaction, and FailOutbox is a trigger on the outbox table
// that refuses the named event's insert.
func atomic(t *testing.T, admin *sql.DB, tx db.Tx[db.Tenant]) *tasktest.Atomic {
	return &tasktest.Atomic{
		Attempt: func(do func(ctx context.Context, tx db.Tx[db.Tenant]) error) error {
			ctx := tasktest.As(tenancy.WithTenant(t.Context(), acme), tasktest.Holder)
			if err := tx.DB().Exec(`SAVEPOINT attempt`).Error; err != nil {
				t.Fatalf("savepoint: %v", err)
			}
			err := do(ctx, tx)
			if err != nil {
				if rollback := tx.DB().Exec(`ROLLBACK TO SAVEPOINT attempt`).Error; rollback != nil {
					t.Fatalf("rollback to savepoint: %v", rollback)
				}
			}
			return err
		},
		FailOutbox: func(event string) func() {
			set := func(name string) {
				if err := tx.DB().Exec(`SELECT set_config('task.refuse_event', ?, false)`, name).Error; err != nil {
					t.Fatalf("set the refused event: %v", err)
				}
			}
			set(event)
			return func() { set("") }
		},
		Wrote: func(_ context.Context, tx db.Tx[db.Tenant]) error {
			return tx.DB().Exec(`INSERT INTO callers_rows (id, tenant_id) VALUES (?, ?)`, uuid.New(), acme.ID).Error
		},
		Rows: func() int { return count(t, tx, "callers_rows") },
	}
}

// refuseNamedEvent installs, before the case's transaction opens, a trigger that
// refuses an outbox insert whose event name is the one the transaction's
// task.refuse_event setting holds. Installing it inside the case would wait on
// locks the case's own transaction holds.
func refuseNamedEvent(t *testing.T, admin *sql.DB) {
	mustExec(t, admin, `CREATE FUNCTION refuse_event() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			IF NEW.name = current_setting('task.refuse_event', true) THEN
				RAISE EXCEPTION 'outbox refuses %', NEW.name;
			END IF;
			RETURN NEW;
		END $$`)
	mustExec(t, admin, `CREATE TRIGGER refuse_event BEFORE INSERT ON `+outbox+` FOR EACH ROW EXECUTE FUNCTION refuse_event()`)
}
