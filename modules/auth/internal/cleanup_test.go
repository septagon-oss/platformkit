package internal

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/migrations"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/user"
)

func TestDetachedAuthWritesSurviveCancellation(t *testing.T) {
	admin, conn, ctx, hash := authCleanupFixture(t, 2)
	rollback := errors.New("the request was refused")
	err := db.Run(ctx, conn, func(ctx context.Context, _ db.Tx[db.Tenant]) error {
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		svc := new(Service)
		svc.forget(canceled, hash)
		svc.recordFailure(canceled, "ADA@example.com", contracts.Client{}, false)
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("request rollback: %v", err)
	}
	var sessions, failures int
	err = admin.QueryRowContext(t.Context(), `SELECT
		(SELECT count(*) FROM sessions),
		(SELECT count(*) FROM platformkit_outbox WHERE name = 'auth.login_failed'
		 AND payload->>'email' = 'ada@example.com')`).Scan(&sessions, &failures)
	if err != nil || sessions != 0 || failures != 1 {
		t.Fatalf("detached writes after cancellation and rollback: sessions=%d failures=%d err=%v", sessions, failures, err)
	}
}

func TestDetachedAuthWritesBoundPoolWait(t *testing.T) {
	for _, name := range []string{"expired session", "failed login"} {
		t.Run(name, func(t *testing.T) {
			admin, conn, ctx, hash := authCleanupFixture(t, 1)
			rollback := errors.New("the request was refused")
			done := make(chan struct{})
			err := db.Run(ctx, conn, func(ctx context.Context, _ db.Tx[db.Tenant]) error {
				// This transaction owns the pool's only connection. The detached
				// write must stop waiting even though its parent is already canceled.
				canceled, cancel := context.WithCancel(ctx)
				cancel()
				go func() {
					defer close(done)
					svc := new(Service)
					if name == "expired session" {
						svc.forget(canceled, hash)
					} else {
						svc.recordFailure(canceled, "ada@example.com", contracts.Client{}, false)
					}
				}()
				select {
				case <-done:
				case <-time.After(3 * time.Second):
					t.Error("detached auth write exceeded its two-second pool wait budget")
				}
				return rollback
			})
			if !errors.Is(err, rollback) {
				t.Fatalf("request rollback: %v", err)
			}
			// Also join the old, unbounded implementation after the watchdog has
			// released the caller's connection, so a red test leaves no waiter.
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("detached auth write did not finish after releasing the pool")
			}
			if stats := conn.Stats(); stats.WaitCount == 0 || stats.InUse != 0 {
				t.Errorf("pool wait was not exercised or a connection remains in use: %+v", stats)
			}
			var sessions, failures int
			err = admin.QueryRowContext(t.Context(), `SELECT
				(SELECT count(*) FROM sessions),
				(SELECT count(*) FROM platformkit_outbox WHERE name = 'auth.login_failed')`).Scan(&sessions, &failures)
			if err != nil || sessions != 1 || failures != 0 {
				t.Errorf("timed-out write changed committed state: sessions=%d failures=%d err=%v", sessions, failures, err)
			}
		})
	}
}

// authCleanupFixture uses the existing schema harness and real user service.
// A one-connection pool is intentional: the request exhausts its own pool.
func authCleanupFixture(t *testing.T, maxOpen int) (*sql.DB, *db.Conn, context.Context, contracts.Digest) {
	t.Helper()
	adminURL, appURL := dbtest.URLs(t)
	if err := db.Migrate(t.Context(), adminURL, migrations.Source); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	admin := dbtest.Open(t, adminURL)
	conn, err := db.OpenWithPool(t.Context(), appURL, db.Pool{MaxOpenConns: maxOpen, MaxIdleConns: maxOpen})
	if err != nil {
		t.Fatalf("open application pool: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "cleanup", Name: "Cleanup"}
	ctx := httpx.WithConn(tenancy.WithTenant(t.Context(), tenant), conn)
	hash := contracts.Hash(uuid.NewString())
	users, _ := user.Module(user.Deps{})
	err = db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		person, err := users.Invite(ctx, tx, "ada@example.com", "Ada")
		if err != nil {
			return err
		}
		return tx.DB().Create(&contracts.Session{
			IDHash: hash, TenantID: tenant.ID, UserID: person.ID,
			CreatedAt: db.Now().Add(-time.Hour), ExpiresAt: db.Now().Add(-time.Minute), LastSeenAt: db.Now().Add(-time.Hour),
		}).Error
	})
	if err != nil {
		t.Fatalf("seed expired session: %v", err)
	}
	return admin, conn, ctx, hash
}
