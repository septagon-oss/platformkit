package contracts_test

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/limit"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/user"
)

func TestVerificationRecipientLimitIsSharedAndTenantScoped(t *testing.T) {
	admin, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	acme := httpx.WithConn(tenancy.WithTenant(t.Context(), tenancy.Tenant{ID: uuid.New()}), conn)
	globex := httpx.WithConn(tenancy.WithTenant(t.Context(), tenancy.Tenant{ID: uuid.New()}), conn)
	first := contracts.NewLimiter(limit.Postgres(httpx.ConnFrom))
	second := contracts.NewLimiter(limit.Postgres(httpx.ConnFrom))
	for _, request := range []struct {
		limiter *contracts.Limiter
		ctx     context.Context
		email   string
		allowed bool
	}{
		{first, acme, " Absent@EXAMPLE.com ", true},
		{second, acme, "absent@example.com", false},
		{second, globex, "ABSENT@example.com", true},
		{first, acme, "another@example.com", true},
	} {
		allowed, err := request.limiter.VerificationMail(request.ctx, request.email)
		if err != nil || allowed != request.allowed {
			t.Fatalf("recipient reservation allowed=%v want=%v error=%v", allowed, request.allowed, err)
		}
	}
	var users, exposed int
	if err := admin.QueryRow("SELECT count(*) FROM users").Scan(&users); err != nil || users != 0 {
		t.Fatal("recipient limits must work without an existing account")
	}
	if err := admin.QueryRow("SELECT count(*) FROM platformkit_limits WHERE key LIKE '%@%'").Scan(&exposed); err != nil || exposed != 0 {
		t.Fatal("recipient limiter persisted a plaintext email key")
	}
	if _, err := admin.Exec("UPDATE platformkit_limits SET window_start = clock_timestamp() - interval '61 seconds'"); err != nil {
		t.Fatal(err)
	}
	if allowed, err := second.VerificationMail(acme, "absent@example.com"); err != nil || !allowed {
		t.Fatal("recipient cooldown did not end after its one-minute window")
	}

	start, results := make(chan struct{}), make(chan bool, 2)
	var replicas sync.WaitGroup
	for _, replica := range []*contracts.Limiter{first, second} {
		replicas.Go(func() {
			<-start
			allowed, err := replica.VerificationMail(acme, "race@example.com")
			if err != nil {
				t.Error(err)
			}
			results <- allowed
		})
	}
	close(start)
	replicas.Wait()
	close(results)
	reservations := 0
	for allowed := range results {
		if allowed {
			reservations++
		}
	}
	if reservations != 1 {
		t.Fatalf("simultaneous recipient reservations = %d, want one", reservations)
	}

	if _, err := admin.Exec("ALTER TABLE platformkit_limits RENAME TO unavailable_limits"); err != nil {
		t.Fatal(err)
	}
	if allowed, err := first.VerificationMail(acme, "outage@example.com"); err == nil || allowed {
		t.Fatal("an unavailable recipient store allowed email delivery")
	}
	if _, err := admin.Exec("ALTER TABLE unavailable_limits RENAME TO platformkit_limits"); err != nil {
		t.Fatal(err)
	}
	if allowed, err := second.VerificationMail(acme, "outage@example.com"); err != nil || !allowed {
		t.Fatal("failed recipient reservation remained reserved after store recovery")
	}
}
