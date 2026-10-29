package task_test

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/task"
	"github.com/septagon-oss/platformkit/modules/task/contracts"
	"github.com/septagon-oss/platformkit/modules/task/contracts/tasktest"
)

func TestPolicyProvidersReadTheRowAfterWaitingForItsWriter(t *testing.T) {
	for _, adapter := range []string{"opa", "topaz"} {
		t.Run(adapter, func(t *testing.T) {
			p := newPolicyLeg(t, adapter)
			admin, conn := dbtest.Schema(t, task.Migrations)
			ctx, cancel := context.WithTimeout(tasktest.As(tenancy.WithTenant(t.Context(), acme), uuid.New()), 10*time.Second)
			defer cancel()
			f := policySQLWorld(t, ctx, p, false, admin, conn)
			row, other := f.Seed(&contracts.Task{Title: "waiting resolve"}), uuid.New()
			svc := task.NewServiceWithPolicy(p)
			locked, started := make(chan int, 1), make(chan int, 1)
			release := make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			first, next := make(chan error, 1), make(chan error, 1)
			var workers sync.WaitGroup
			defer func() { unblock(); cancel(); workers.Wait() }()
			workers.Go(func() {
				first <- db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
					if _, err := svc.Assign(ctx, tx, row, other); err != nil {
						return err
					}
					var pid int
					if err := tx.DB().Raw("SELECT pg_backend_pid()").Scan(&pid).Error; err != nil {
						return err
					}
					locked <- pid
					select {
					case <-release:
						return nil
					case <-ctx.Done():
						return ctx.Err()
					}
				})
			})
			var holder int
			select {
			case holder = <-locked:
			case err := <-first:
				t.Fatalf("holder: %v", err)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			workers.Go(func() {
				next <- db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
					var pid int
					if err := tx.DB().Raw("SELECT pg_backend_pid()").Scan(&pid).Error; err != nil {
						return err
					}
					started <- pid
					got, err := svc.Resolve(ctx, tx, row, "done")
					if got != nil {
						t.Error("refused waiter returned a task")
					}
					return err
				})
			})
			var contender int
			select {
			case contender = <-started:
			case err := <-next:
				t.Fatalf("contender: %v", err)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			for {
				var blocked bool
				if err := admin.QueryRowContext(ctx, "SELECT $1 = ANY(pg_blocking_pids($2))", holder, contender).Scan(&blocked); err != nil {
					t.Fatal(err)
				}
				if blocked {
					break
				}
				select {
				case <-time.After(10 * time.Millisecond):
				case err := <-next:
					t.Fatalf("did not wait: %v", err)
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			unblock()
			if err := <-first; err != nil {
				t.Fatal(err)
			}
			if err := <-next; !errors.Is(err, tenancy.ErrPolicyDenied) {
				t.Fatalf("waiting resolver = %v", err)
			}
			got, err := f.Task(row)
			if err != nil || got.Status != contracts.StatusAcknowledged || got.AssigneeID == nil || *got.AssigneeID != other || got.Revision != 2 || !reflect.DeepEqual(f.Published(), []string{contracts.EventAssigned}) {
				t.Fatalf("settled task = %+v, events %v, error %v", got, f.Published(), err)
			}
			last := p.calls()[len(p.calls())-1]
			if last.Resource.Attributes["assignee_id"] != other.String() {
				t.Fatalf("waiter sent stale facts: %+v", last)
			}
		})
	}
}

func TestPolicyRefusalAndOutboxFailureRollbackTheCallersWrites(t *testing.T) {
	for _, adapter := range []string{"opa", "topaz"} {
		for _, failure := range []string{"policy denial", "outbox failure"} {
			for _, command := range []string{"assign", "resolve"} {
				for _, world := range []string{"sql", "http"} {
					t.Run(adapter+"/"+failure+"/"+command+"/"+world, func(t *testing.T) {
						p := newPolicyLeg(t, adapter)
						admin, conn := dbtest.Schema(t, task.Migrations)
						ctx := tasktest.As(tenancy.WithTenant(t.Context(), acme), uuid.New())
						f := policySQLWorld(t, ctx, p, false, admin, conn)
						row, caller := f.Seed(&contracts.Task{Title: "protected"}), f.Seed(&contracts.Task{Title: "caller state"})
						before, _ := f.Task(row)
						if failure == "policy denial" {
							p.use(tasktest.DeniedPolicy)
						} else {
							for _, sql := range []string{
								`CREATE FUNCTION refuse_resolution() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.name IN ('task.resolved', 'task.assigned') THEN RAISE EXCEPTION 'injected outbox failure'; END IF; RETURN NEW; END $$`,
								`CREATE TRIGGER refuse_resolution BEFORE INSERT ON platformkit_outbox FOR EACH ROW EXECUTE FUNCTION refuse_resolution()`,
							} {
								if _, err := admin.ExecContext(ctx, sql); err != nil {
									t.Fatal(err)
								}
							}
						}
						writeCaller := func(_ context.Context, tx db.Tx[db.Tenant]) error {
							return tx.DB().Exec("UPDATE tasks SET description = 'caller write' WHERE id = ?", caller).Error
						}
						var err error
						if world == "http" {
							who := uuid.Nil
							if command == "assign" {
								who = uuid.New()
							}
							got, failed := policyHTTP(t, conn, p, ctx, row, command, who, "done", writeCaller)
							err = failed
							if got != nil {
								t.Errorf("refused HTTP command returned task: %+v", got)
							}
						} else {
							err = db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
								if err := writeCaller(ctx, tx); err != nil {
									return err
								}
								var got *contracts.Task
								var err error
								service := task.NewServiceWithPolicy(p)
								if command == "assign" {
									got, err = service.Assign(ctx, tx, row, uuid.New())
								} else {
									got, err = service.Resolve(ctx, tx, row, "done")
								}
								if got != nil {
									t.Errorf("refused command returned task: %+v", got)
								}
								return err
							})
						}
						if err == nil {
							t.Fatal("injected refusal succeeded")
						}
						if failure == "policy denial" && !errors.Is(err, tenancy.ErrPolicyDenied) {
							t.Fatalf("denial = %v", err)
						}
						after, readErr := f.Task(row)
						owned, callerErr := f.Task(caller)
						if readErr != nil || callerErr != nil || !reflect.DeepEqual(before, after) || owned.Description != "" || len(f.Published()) != 0 {
							t.Fatalf("partial commit: task %+v, caller %+v, events %v, errors %v %v", after, owned, f.Published(), readErr, callerErr)
						}
					})
				}
			}
		}
	}
}
