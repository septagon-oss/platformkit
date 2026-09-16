package internal_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/migrations"
	"github.com/septagon-oss/platformkit/modules/task"
	"github.com/septagon-oss/platformkit/modules/task/contracts"
	"github.com/septagon-oss/platformkit/modules/task/internal"
)

type taskCommand func(context.Context, db.Tx[db.Tenant], uuid.UUID) (*contracts.Task, error)

// These expectations describe committed business facts, not SQL spelling. The
// holder has changed the row but not committed when the contender starts.
func TestConcurrentTaskCommands(t *testing.T) {
	svc := internal.NewService()
	who := uuid.New()
	assign := func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*contracts.Task, error) {
		return svc.Assign(ctx, tx, id, who)
	}
	resolve := func(note string) taskCommand {
		return func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*contracts.Task, error) {
			return svc.Resolve(ctx, tx, id, note)
		}
	}
	patch := func(column string, value any) taskCommand {
		return func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*contracts.Task, error) {
			err := tx.DB().Model(&contracts.Task{}).Where("id = ?", id).Update(column, value).Error
			return nil, err
		}
	}
	for _, isolation := range []string{"read committed", "repeatable read"} {
		t.Run(isolation, func(t *testing.T) {
			for _, tc := range []struct {
				name        string
				first, next taskCommand
				rollback    bool
				dueLater    bool
				wantErr     error
				status      string
				assigned    bool
				breached    bool
				deleted     bool
				description string
				events      []string
			}{
				{name: "same assignment", first: assign, next: assign, status: contracts.StatusAcknowledged,
					assigned: true, events: []string{contracts.EventAssigned}},
				{name: "same resolution", first: resolve("valve replaced"), next: resolve("valve replaced"),
					status: contracts.StatusResolved, events: []string{contracts.EventResolved}},
				{name: "different resolution", first: resolve("valve replaced"), next: resolve("inspected only"),
					wantErr: crud.ErrConflict, status: contracts.StatusResolved, events: []string{contracts.EventResolved}},
				{name: "assignment after resolution", first: resolve("valve replaced"), next: assign,
					wantErr: crud.ErrConflict, status: contracts.StatusResolved, events: []string{contracts.EventResolved}},
				{name: "SLA check after resolution", first: resolve("valve replaced"), next: svc.CheckSLA,
					status: contracts.StatusResolved, events: []string{contracts.EventResolved}},
				{name: "same breach", first: svc.CheckSLA, next: svc.CheckSLA,
					status: contracts.StatusOpen, breached: true, events: []string{contracts.EventSLABreached}},
				{name: "deadline extended", first: patch("sla_deadline", time.Now().Add(time.Hour)), next: svc.CheckSLA,
					status: contracts.StatusOpen},
				{name: "deadline removed", first: patch("sla_deadline", nil), next: svc.CheckSLA,
					status: contracts.StatusOpen},
				{name: "deadline shortened", first: patch("sla_deadline", time.Now().Add(-time.Hour)), next: svc.CheckSLA,
					dueLater: true, status: contracts.StatusOpen, breached: true, events: []string{contracts.EventSLABreached}},
				{name: "resolution retains assignment", first: assign, next: resolve("valve replaced"),
					status: contracts.StatusResolved, assigned: true, events: []string{contracts.EventAssigned, contracts.EventResolved}},
				{name: "description survives assignment", first: patch("description", "the other patch"), next: assign,
					status: contracts.StatusAcknowledged, assigned: true, description: "the other patch", events: []string{contracts.EventAssigned}},
				{name: "assignment after deletion", first: patch("deleted_at", time.Now()), next: assign,
					deleted: true, wantErr: crud.ErrNotFound},
				{name: "resolution after deletion", first: patch("deleted_at", time.Now()), next: resolve("valve replaced"),
					deleted: true, wantErr: crud.ErrNotFound},
				{name: "SLA check after deletion", first: patch("deleted_at", time.Now()), next: svc.CheckSLA,
					deleted: true, wantErr: crud.ErrNotFound},
				{name: "rolled back assignment", first: assign, next: assign, rollback: true,
					status: contracts.StatusAcknowledged, assigned: true, events: []string{contracts.EventAssigned}},
				{name: "rolled back resolution", first: resolve("uncommitted account"), next: resolve("valve replaced"), rollback: true,
					status: contracts.StatusResolved, events: []string{contracts.EventResolved}},
				{name: "rolled back breach", first: svc.CheckSLA, next: svc.CheckSLA, rollback: true,
					status: contracts.StatusOpen, breached: true, events: []string{contracts.EventSLABreached}},
			} {
				t.Run(tc.name, func(t *testing.T) {
					admin, conn := taskIsolationSchema(t, isolation)
					ctx := tenancy.WithTenant(t.Context(), acme)
					row := &contracts.Task{Title: "chiller", SLADeadline: new(time.Now().Add(-time.Hour))}
					if tc.dueLater {
						row.SLADeadline = new(time.Now().Add(time.Hour))
					}
					if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
						return crud.Create(ctx, tx, row)
					}); err != nil {
						t.Fatal(err)
					}
					var returned *contracts.Task
					err := runBlockedTaskWriter(t, admin, conn, tc.rollback, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
						_, err := tc.first(ctx, tx, row.ID)
						return err
					}, func(ctx context.Context, started chan<- int) error {
						return db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
							if err := reportTaskBackend(tx, started); err != nil {
								return err
							}
							var err error
							returned, err = tc.next(ctx, tx, row.ID)
							return err
						})
					})
					if isolation == "repeatable read" && !tc.rollback {
						pg, ok := errors.AsType[*pgconn.PgError](err)
						if !ok || pg.Code != "40001" {
							t.Fatalf("stale snapshot = %v, want SQLSTATE 40001", err)
						}
						// The caller retries the whole transaction; no command hides
						// a serialization failure inside the old snapshot.
						err = db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
							var err error
							returned, err = tc.next(ctx, tx, row.ID)
							return err
						})
					}
					if !errors.Is(err, tc.wantErr) {
						t.Fatalf("contender = %v, want %v", err, tc.wantErr)
					}
					var saved *contracts.Task
					err = db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
						var err error
						saved, err = crud.Get[*contracts.Task](tx, row.ID)
						return err
					})
					if tc.deleted {
						if !errors.Is(err, crud.ErrNotFound) {
							t.Fatalf("deleted task = %v, %v", saved, err)
						}
					} else {
						if err != nil {
							t.Fatal(err)
						}
						if err := saved.Validate(ctx); err != nil {
							t.Fatalf("committed an invalid task: %v", err)
						}
						if saved.Status != tc.status || (saved.AssigneeID != nil) != tc.assigned ||
							saved.SLABreached != tc.breached || saved.Description != tc.description {
							t.Fatalf("unexpected committed state: %+v", saved)
						}
						if tc.assigned && *saved.AssigneeID != who {
							t.Errorf("assignee = %s, want %s", saved.AssigneeID, who)
						}
						if tc.status == contracts.StatusResolved && saved.Resolution != "valve replaced" {
							t.Errorf("resolution overwritten: %q", saved.Resolution)
						}
						if tc.wantErr == nil && (returned == nil || returned.Status != saved.Status || returned.SLABreached != saved.SLABreached ||
							returned.Description != saved.Description || returned.Resolution != saved.Resolution) {
							t.Errorf("returned stale state: %+v; committed: %+v", returned, saved)
						}
						if tc.wantErr == nil && tc.assigned && (returned == nil || returned.AssigneeID == nil || *returned.AssigneeID != who) {
							t.Errorf("returned assignment differs from stored assignee: %+v", returned)
						}
					}
					assertTaskEvents(t, admin, tc.events)
				})
			}
		})
	}
}

// Exercise the actual module's CRUD registration. Locking only lifecycle
// commands leaves generic PATCH validation and DELETE event snapshots stale.
func TestTaskRESTWaitsForCommittedState(t *testing.T) {
	svc := internal.NewService()
	resolve := func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*contracts.Task, error) {
		return svc.Resolve(ctx, tx, id, "valve replaced")
	}
	for _, tc := range []struct {
		name, method, body string
		first              taskCommand
		wantCode           int
		events             []string
	}{
		{"reopening a resolved task", http.MethodPatch, `{"status":"in_progress"}`, resolve, 422,
			[]string{contracts.EventResolved}},
		{"removing a breached deadline", http.MethodPatch, `{"slaDeadline":null}`, svc.CheckSLA, 422,
			[]string{contracts.EventSLABreached}},
		{"description patch returns resolution", http.MethodPatch, `{"description":"operator note"}`, resolve, 200,
			[]string{contracts.EventResolved, contracts.EventUpdated}},
		{"deletion records resolved state", http.MethodDelete, "", resolve, 204,
			[]string{contracts.EventResolved, contracts.EventDeleted}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			admin, conn := dbtest.Schema(t, task.Migrations)
			ctx := tenancy.WithTenant(t.Context(), acme)
			row := &contracts.Task{Title: "chiller", SLADeadline: new(time.Now().Add(-time.Hour))}
			if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				return crud.Create(ctx, tx, row)
			}); err != nil {
				t.Fatal(err)
			}
			var response *httptest.ResponseRecorder
			err := runBlockedTaskWriter(t, admin, conn, false, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				_, err := tc.first(ctx, tx, row.ID)
				return err
			}, func(ctx context.Context, started chan<- int) error {
				api, router := httpx.New(httpx.Options{
					PublicHost: host, Tenants: caller{}, Conn: conn, Authorize: caller{},
					Authenticate: func(_ context.Context, tx db.Tx[db.Tenant], _ *http.Request) (tenancy.Principal, bool, error) {
						return tenancy.Principal{UserID: uuid.New()}, true, reportTaskBackend(tx, started)
					},
					Log: slog.New(slog.DiscardHandler),
				})
				task.Module(task.Deps{}).Routes(api)
				if err := api.ValidateDeclarations(); err != nil {
					return err
				}
				req := httptest.NewRequestWithContext(ctx, tc.method, "http://"+host+path+"/"+row.ID.String(), strings.NewReader(tc.body))
				req.Header.Set("Content-Type", "application/json")
				req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
				response = httptest.NewRecorder()
				router.ServeHTTP(response, req)
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if response.Code != tc.wantCode {
				t.Fatalf("response = %d %s, want %d", response.Code, response.Body, tc.wantCode)
			}
			var saved contracts.Task
			if err := db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
				return tx.DB().Where("id = ?", row.ID).Take(&saved).Error
			}); err != nil {
				t.Fatal(err)
			}
			if err := saved.Validate(ctx); err != nil {
				t.Fatalf("committed an invalid task: %v", err)
			}
			if tc.wantCode == 200 {
				var returned contracts.Task
				if err := json.Unmarshal(response.Body.Bytes(), &returned); err != nil {
					t.Fatal(err)
				}
				if returned.Status != contracts.StatusResolved || returned.Resolution != "valve replaced" ||
					returned.ResolvedAt == nil || returned.Description != "operator note" || saved.Description != "operator note" {
					t.Fatalf("PATCH returned stale facts: %+v; stored: %+v", returned, saved)
				}
			}
			if (saved.DeletedAt != nil) != (tc.method == http.MethodDelete) {
				t.Errorf("deleted at = %v for %s", saved.DeletedAt, tc.method)
			}
			assertTaskEvents(t, admin, tc.events)
			if tc.wantCode == 200 || tc.wantCode == 204 {
				var payload []byte
				if err := admin.QueryRowContext(ctx, "SELECT payload FROM platformkit_outbox WHERE name = $1", tc.events[1]).Scan(&payload); err != nil {
					t.Fatal(err)
				}
				var event contracts.Task
				if err := json.Unmarshal(payload, &event); err != nil {
					t.Fatal(err)
				}
				if event.Status != contracts.StatusResolved || event.Resolution != "valve replaced" || event.ResolvedAt == nil {
					t.Errorf("event carries stale facts: %s", payload)
				}
			}
		})
	}
}

func taskIsolationSchema(t *testing.T, isolation string) (*sql.DB, *db.Conn) {
	t.Helper()
	adminURL, appURL := dbtest.URLs(t)
	if err := db.Migrate(t.Context(), adminURL, migrations.Source, task.Migrations); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(appURL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("default_transaction_isolation", isolation)
	u.RawQuery = q.Encode()
	conn, err := db.Open(t.Context(), u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return dbtest.Open(t, adminURL), conn
}

// runBlockedTaskWriter waits until PostgreSQL identifies this exact holder as
// the contender's blocker. All goroutines finish before schema cleanup, even
// when an assertion fails; no other test's UPDATE can satisfy the barrier.
func runBlockedTaskWriter(t *testing.T, admin *sql.DB, conn *db.Conn, rollback bool,
	first func(context.Context, db.Tx[db.Tenant]) error,
	next func(context.Context, chan<- int) error,
) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(tenancy.WithTenant(t.Context(), acme), 10*time.Second)
	locked, started := make(chan int, 1), make(chan int, 1)
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	firstDone, nextDone := make(chan error, 1), make(chan error, 1)
	var workers sync.WaitGroup
	defer func() { unblock(); cancel(); workers.Wait() }()
	workers.Go(func() {
		firstDone <- db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			if err := first(ctx, tx); err != nil {
				return err
			}
			if err := reportTaskBackend(tx, locked); err != nil {
				return err
			}
			select {
			case <-release:
				if rollback {
					return errRollback
				}
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	})
	awaitPID := func(ready <-chan int, done <-chan error) int {
		select {
		case pid := <-ready:
			return pid
		case err := <-done:
			t.Fatalf("writer completed before the barrier: %v", err)
		case <-ctx.Done():
			t.Fatalf("writer did not reach the barrier: %v", ctx.Err())
		}
		return 0
	}
	holderPID := awaitPID(locked, firstDone)
	workers.Go(func() { nextDone <- next(ctx, started) })
	contenderPID := awaitPID(started, nextDone)
	for tick := time.Tick(10 * time.Millisecond); ; {
		var blocked bool
		if err := admin.QueryRowContext(ctx, "SELECT $1 = ANY(pg_blocking_pids($2))", holderPID, contenderPID).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		select {
		case err := <-nextDone:
			t.Fatalf("contender completed without waiting for the holder: %v", err)
		case <-ctx.Done():
			t.Fatalf("contender never blocked on the holder: %v", ctx.Err())
		case <-tick:
		}
	}
	unblock()
	if err := <-firstDone; (rollback && !errors.Is(err, errRollback)) || (!rollback && err != nil) {
		t.Fatalf("holder transaction: %v", err)
	}
	return <-nextDone
}

func reportTaskBackend(tx db.Tx[db.Tenant], started chan<- int) error {
	var pid int
	if err := tx.DB().Raw("SELECT pg_backend_pid()").Scan(&pid).Error; err != nil {
		return fmt.Errorf("identify task transaction: %w", err)
	}
	started <- pid
	return nil
}

func assertTaskEvents(t *testing.T, admin *sql.DB, want []string) {
	t.Helper()
	rows, err := admin.QueryContext(t.Context(), "SELECT name FROM platformkit_outbox WHERE tenant_id = $1 ORDER BY name", acme.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		got = append(got, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, slices.Sorted(slices.Values(want))) {
		t.Errorf("committed events = %v, want %v", got, want)
	}
}
