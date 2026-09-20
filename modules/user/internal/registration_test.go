package internal_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/user"
	"github.com/septagon-oss/platformkit/modules/user/contracts"
)

func TestPendingRegistrationsKeepTenantOwnershipAndRollBackTogether(t *testing.T) {
	admin, conn := dbtest.Schema(t, user.Migrations)
	svc := newService()
	const password = "correct horse battery staple"
	var ids []uuid.UUID
	for _, tenant := range []tenancy.Tenant{acme, globex} {
		if err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			u, err := svc.RegisterPending(ctx, tx, contracts.PendingRegistration{Email: "same@example.com", Password: password})
			if err == nil {
				ids = append(ids, u.ID)
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := svc.RegisterPending(ctx, tx, contracts.PendingRegistration{Email: "SAME@example.com", Password: password + " changed", Roles: []string{"admin"}})
		return err
	})
	if !errors.Is(err, crud.ErrConflict) {
		t.Fatalf("duplicate registration = %v", err)
	}
	if err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		page, err := svc.PendingRegistrations(ctx, tx, 0, 0)
		if err != nil {
			return err
		}
		if page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != ids[0] || len(page.Items[0].Roles) != 0 || !page.Items[0].CheckPassword(password) {
			t.Fatal("duplicate signup or tenant read altered the original registration")
		}
		_, err = svc.ApproveRegistration(ctx, tx, ids[1], uuid.New())
		if !errors.Is(err, crud.ErrNotFound) {
			t.Fatalf("cross-tenant approval = %v", err)
		}
		_, err = svc.ApproveRegistration(ctx, tx, ids[0], uuid.New())
		if err != nil {
			return err
		}
		return errRollback
	}); !errors.Is(err, errRollback) {
		t.Fatalf("approval rollback = %v", err)
	}
	var count int
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM users WHERE status = 'pending'").Scan(&count); err != nil || count != 2 {
		t.Fatalf("rollback did not retain both pending users: %d, %v", count, err)
	}
	rows, err := admin.QueryContext(t.Context(), "SELECT name, payload::text FROM platformkit_outbox")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var name, payload string
		if err := rows.Scan(&name, &payload); err != nil {
			t.Fatal(err)
		}
		if name != contracts.EventRegistrationPending || strings.Contains(payload, password) || strings.Contains(payload, "argon2id") {
			t.Fatal("registration leaked credentials or published a rolled-back approval")
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentLifecycleChangesCannotBeUndoneByActivation(t *testing.T) {
	for _, change := range []struct{ operation, field string }{{"password", "status"}, {"approval", "status"}, {"verification", "status"}, {"verification", "email"}} {
		t.Run(change.operation+"/"+change.field, func(t *testing.T) {
			admin, conn := dbtest.Schema(t, user.Migrations)
			svc := newService()
			var id uuid.UUID
			if err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				register := svc.RegisterPending
				if change.operation == "verification" {
					register = svc.RegisterUnverified
				}
				u, err := register(ctx, tx, contracts.PasswordRegistration{Email: "pending@example.com", Password: "correct horse battery staple"})
				if err != nil {
					return err
				}
				id = u.ID
				if change.operation == "password" {
					_, err = svc.ApproveRegistration(ctx, tx, id, uuid.New())
				}
				return err
			}); err != nil {
				t.Fatal(err)
			}
			locked, release := make(chan int, 1), make(chan struct{})
			unlock := sync.OnceFunc(func() { close(release) })
			defer unlock()
			deactivated, attempted := make(chan error, 1), make(chan error, 1)
			var wg sync.WaitGroup
			defer func() { unlock(); wg.Wait() }()
			wg.Go(func() {
				deactivated <- db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
					if change.field == "email" {
						if err := tx.DB().Model(&contracts.User{}).Where("id = ?", id).Update("email", "changed@example.com").Error; err != nil {
							return err
						}
					} else {
						if _, err := svc.Deactivate(ctx, tx, id); err != nil {
							return err
						}
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
			var pid int
			select {
			case pid = <-locked:
			case err := <-deactivated:
				t.Fatalf("deactivation failed before acquiring its lock: %v", err)
			case <-t.Context().Done():
				t.Fatal("test context ended")
			}
			wg.Go(func() {
				attempted <- db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
					if change.operation == "password" {
						return svc.SetPassword(ctx, tx, id, "correct horse battery staple changed")
					}
					if change.operation == "verification" {
						_, err := svc.VerifyEmail(ctx, tx, id, "pending@example.com")
						return err
					}
					_, err := svc.ApproveRegistration(ctx, tx, id, uuid.New())
					return err
				})
			})
			blocked := false
			for range 500 {
				if err := admin.QueryRowContext(t.Context(), "SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE $1 = ANY(pg_blocking_pids(pid)))", pid).Scan(&blocked); err != nil {
					t.Fatal(err)
				}
				if blocked {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if !blocked {
				t.Fatal("the competing lifecycle command never waited on deactivation")
			}
			unlock()
			if err := <-deactivated; err != nil {
				t.Fatal(err)
			}
			if err := <-attempted; !errors.Is(err, crud.ErrConflict) {
				t.Fatalf("%s after concurrent %s change = %v", change.operation, change.field, err)
			}
			wantStatus, wantEmail := contracts.StatusInactive, "pending@example.com"
			if change.field == "email" {
				wantStatus, wantEmail = contracts.StatusUnverified, "changed@example.com"
			}
			var status, email string
			if err := admin.QueryRowContext(t.Context(), "SELECT status, email FROM users WHERE id = $1", id).Scan(&status, &email); err != nil || status != wantStatus || email != wantEmail {
				t.Fatalf("lifecycle change was undone: %s, %v", status, err)
			}
		})
	}
}
