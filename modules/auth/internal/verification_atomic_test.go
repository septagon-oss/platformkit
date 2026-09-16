package internal_test

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/notification"
	usermodule "github.com/septagon-oss/platformkit/modules/user"
	user "github.com/septagon-oss/platformkit/modules/user/contracts"
)

func TestConcurrentEmailVerificationConsumesOnlyOnce(t *testing.T) {
	admin, conn := dbtest.Schema(t, usermodule.Migrations, notification.Migrations, auth.Migrations)
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, emailSignup)
	u, token := verificationSignup(t, conn, router, "concurrent@example.com")
	body := verificationBody(t, token)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	start := make(chan struct{})
	statuses := make(chan int, 2)
	var callers sync.WaitGroup
	for range 2 {
		callers.Go(func() {
			<-start
			res := call(t, router, http.MethodPost, "/api/v1/auth/verify-email", body, func(r *http.Request) {
				*r = *r.WithContext(ctx)
			})
			if len(res.Result().Cookies()) != 0 {
				t.Error("verification issued a cookie")
			}
			statuses <- res.Code
		})
	}
	close(start)
	callers.Wait()
	close(statuses)
	counts := map[int]int{}
	for status := range statuses {
		counts[status]++
	}
	if counts[http.StatusOK] != 1 || counts[http.StatusUnauthorized] != 1 || len(counts) != 2 {
		t.Fatalf("simultaneous verification statuses = %v", counts)
	}
	assertVerificationState(t, admin, u, user.StatusActive, 0, 1)
}

func TestEmailVerificationRechecksCredentialAfterAdvisoryWait(t *testing.T) {
	for _, change := range []string{"expiry", "rotation"} {
		t.Run(change, func(t *testing.T) {
			admin, conn := dbtest.Schema(t, usermodule.Migrations, notification.Migrations, auth.Migrations)
			router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, emailSignup)
			u, token := verificationSignup(t, conn, router, "waiting@example.com")
			body := verificationBody(t, token)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			blocker, err := admin.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			var holder int
			if err := blocker.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&holder); err != nil {
				_ = blocker.Rollback()
				t.Fatal(err)
			}
			key := "auth/verification/" + acme.ID.String() + "/" + u.ID.String()
			if _, err := blocker.ExecContext(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", key); err != nil {
				_ = blocker.Rollback()
				t.Fatal(err)
			}
			response := make(chan int, 1)
			var callers sync.WaitGroup
			// Release the database lock before joining, including on a failed
			// assertion. The request itself also has a bounded context.
			defer callers.Wait()
			defer blocker.Rollback()
			callers.Go(func() {
				res := call(t, router, http.MethodPost, "/api/v1/auth/verify-email", body, func(r *http.Request) {
					*r = *r.WithContext(ctx)
				})
				response <- res.Code
			})
			waiter := waitForVerificationBlock(t, ctx, admin, holder)
			const replacement = "disposable-rotated-verification-credential"
			if change == "expiry" {
				if _, err := blocker.ExecContext(ctx, "UPDATE verification_tokens SET expires_at = clock_timestamp() WHERE user_id = $1", u.ID); err != nil {
					t.Fatal(err)
				}
				// The request started while the link was valid. Expiry must be
				// checked against wall time after the wait, not its transaction's
				// frozen now(), which still precedes this deliberately set expiry.
				var afterStart bool
				if err := blocker.QueryRowContext(ctx, "SELECT expires_at > xact_start FROM verification_tokens CROSS JOIN pg_stat_activity WHERE user_id = $1 AND pid = $2", u.ID, waiter).Scan(&afterStart); err != nil || !afterStart {
					t.Fatal("expiry fixture did not advance beyond the waiting transaction start")
				}
			} else {
				if _, err := blocker.ExecContext(ctx, "UPDATE verification_tokens SET token_hash = $1 WHERE user_id = $2", contracts.Hash(replacement), u.ID); err != nil {
					t.Fatal("could not rotate the disposable verification digest")
				}
			}
			if err := blocker.Commit(); err != nil {
				t.Fatal(err)
			}
			select {
			case status := <-response:
				if status != http.StatusUnauthorized {
					t.Fatalf("verification after waiting for %s = %d", change, status)
				}
			case <-ctx.Done():
				t.Fatal("verification did not finish after its lock was released")
			}
			assertVerificationState(t, admin, u, user.StatusUnverified, 1, 0)
			if change == "rotation" {
				res := call(t, router, http.MethodPost, "/api/v1/auth/verify-email", verificationBody(t, replacement))
				if res.Code != http.StatusOK {
					t.Fatalf("the replacement verification credential = %d", res.Code)
				}
				assertVerificationState(t, admin, u, user.StatusActive, 0, 1)
			}
		})
	}
}

func TestEmailVerificationEventFailureRollsBackConsumption(t *testing.T) {
	admin, conn := dbtest.Schema(t, usermodule.Migrations, notification.Migrations, auth.Migrations)
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, emailSignup)
	u, token := verificationSignup(t, conn, router, "rollback@example.com")
	body := verificationBody(t, token)
	if _, err := admin.ExecContext(t.Context(), "ALTER TABLE platformkit_outbox ADD CONSTRAINT reject_verification_event CHECK (name <> 'user.email_verified')"); err != nil {
		t.Fatal(err)
	}
	res := call(t, router, http.MethodPost, "/api/v1/auth/verify-email", body)
	if res.Code != http.StatusInternalServerError {
		t.Fatalf("verification with a refused durable event = %d", res.Code)
	}
	assertVerificationState(t, admin, u, user.StatusUnverified, 1, 0)
	if _, err := admin.ExecContext(t.Context(), "ALTER TABLE platformkit_outbox DROP CONSTRAINT reject_verification_event"); err != nil {
		t.Fatal(err)
	}
	res = call(t, router, http.MethodPost, "/api/v1/auth/verify-email", body)
	if res.Code != http.StatusOK {
		t.Fatalf("the same credential after rolling back failed delivery of its event = %d", res.Code)
	}
	assertVerificationState(t, admin, u, user.StatusActive, 0, 1)
}

func TestEmailVerificationRechecksAccountAfterIssuance(t *testing.T) {
	for _, change := range []string{"email", "deactivation", "deletion"} {
		t.Run(change, func(t *testing.T) {
			admin, conn := dbtest.Schema(t, usermodule.Migrations, notification.Migrations, auth.Migrations)
			router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, emailSignup)
			u, token := verificationSignup(t, conn, router, "changed@example.com")
			status := user.StatusUnverified
			switch change {
			case "email":
				u.Email = "another@example.com"
				if _, err := admin.ExecContext(t.Context(), "UPDATE users SET email = $1 WHERE id = $2", u.Email, u.ID); err != nil {
					t.Fatal(err)
				}
			case "deactivation":
				status = user.StatusInactive
				if err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
					_, err := realUsers().Deactivate(ctx, tx, u.ID)
					return err
				}); err != nil {
					t.Fatal(err)
				}
			case "deletion":
				if _, err := admin.ExecContext(t.Context(), "UPDATE users SET deleted_at = clock_timestamp() WHERE id = $1", u.ID); err != nil {
					t.Fatal(err)
				}
			}
			res := call(t, router, http.MethodPost, "/api/v1/auth/verify-email", verificationBody(t, token))
			if res.Code != http.StatusUnauthorized || len(res.Result().Cookies()) != 0 {
				t.Fatalf("verification after account %s = %d", change, res.Code)
			}
			assertVerificationState(t, admin, u, status, 1, 0)
		})
	}
}

func waitForVerificationBlock(t *testing.T, ctx context.Context, admin *sql.DB, holder int) int {
	t.Helper()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		var waiter int
		err := admin.QueryRowContext(ctx, "SELECT pid FROM pg_stat_activity WHERE $1 = ANY(pg_blocking_pids(pid)) LIMIT 1", holder).Scan(&waiter)
		if err == nil {
			return waiter
		}
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatal("could not observe the verification request's database lock")
		}
		select {
		case <-tick.C:
		case <-ctx.Done():
			t.Fatal("verification never waited for the tenant/user advisory lock")
		}
	}
}

func assertVerificationState(t *testing.T, admin *sql.DB, original *user.User, status string, tokens, events int) {
	t.Helper()
	var email, actualStatus, passwordHash string
	var roles user.Roles
	var actualTokens, actualEvents, sessions int
	err := admin.QueryRowContext(t.Context(), "SELECT email, status, password_hash, roles FROM users WHERE id = $1", original.ID).
		Scan(&email, &actualStatus, &passwordHash, &roles)
	if err != nil || email != original.Email || actualStatus != status || passwordHash != original.PasswordHash || !slices.Equal(roles, original.Roles) {
		t.Fatal("verification changed account identity, password, roles or the expected lifecycle state")
	}
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM verification_tokens WHERE user_id = $1", original.ID).Scan(&actualTokens); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM platformkit_outbox WHERE name = 'user.email_verified' AND payload->>'userId' = $1", original.ID.String()).Scan(&actualEvents); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM sessions WHERE user_id = $1", original.ID).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if actualTokens != tokens || actualEvents != events || sessions != 0 {
		t.Fatalf("verification persisted tokens=%d events=%d sessions=%d; want %d, %d, 0", actualTokens, actualEvents, sessions, tokens, events)
	}
}
