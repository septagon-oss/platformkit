package internal_test

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/user"
)

func TestConfiguredAccountRequestsShareOneQuotaUnderConcurrency(t *testing.T) {
	admin, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, true, func(d *auth.Deps) {
		d.AccountRequests = contracts.AccountRequestBudget{Quota: 3, Window: time.Hour}
	})
	const ip = "203.0.113.82"
	results := make(chan int, 8)
	start := make(chan struct{})
	var pending sync.WaitGroup
	for range cap(results) {
		pending.Go(func() {
			<-start
			got := call(t, router, http.MethodPost, "/api/v1/public/auth/password/forgot", `{"email":"unknown@example.com"}`, from(ip))
			results <- got.Code
		})
	}
	close(start)
	pending.Wait()
	close(results)
	var allowed, refused int
	for status := range results {
		switch status {
		case http.StatusOK:
			allowed++
		case http.StatusTooManyRequests:
			refused++
		default:
			t.Errorf("concurrent request = %d; want 200 or 429", status)
		}
	}
	if allowed != 3 || refused != 5 {
		t.Fatalf("concurrent requests: allowed=%d refused=%d; want 3 and 5", allowed, refused)
	}
	var before int
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM platformkit_outbox").Scan(&before); err != nil {
		t.Fatal(err)
	}
	got := call(t, router, http.MethodPost, "/api/v1/public/auth/register", `{"email":"uncreated@example.com"}`, from(ip))
	if got.Code != http.StatusTooManyRequests || !strings.Contains(got.Body.String(), "too many account requests") {
		t.Fatalf("registration after the shared quota = %d: %s", got.Code, got.Body)
	}
	var after, users, tokens int
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM platformkit_outbox").Scan(&after); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM users").Scan(&users); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM password_tokens").Scan(&tokens); err != nil {
		t.Fatal(err)
	}
	if after != before || users != 0 || tokens != 0 || len(mailbox.Sent()) != 0 {
		t.Errorf("refusal changed domain state: events before=%d after=%d users=%d tokens=%d mails=%d", before, after, users, tokens, len(mailbox.Sent()))
	}
	if strings.Contains(got.Body.String(), "uncreated@example.com") || len(got.Result().Cookies()) != 0 {
		t.Errorf("refusal returned account data or a cookie: %s", got.Body)
	}
}

func TestConfiguredAccountWindowsKeepTheirTenantAndResetAtTheNamedDuration(t *testing.T) {
	admin, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	_, _, svc := mountConfigured(t, conn, auth.OIDC{}, false, func(d *auth.Deps) {
		d.AccountRequests = contracts.AccountRequestBudget{Quota: 1, Window: time.Hour}
	})
	first := httpx.WithConn(tenancy.WithTenant(t.Context(), acme), conn)
	second := httpx.WithConn(tenancy.WithTenant(t.Context(), globex), conn)
	const ip = "203.0.113.83"
	if got := svc.MayAsk(first, ip); !got.Decided() || !got.Allowed {
		t.Fatalf("first tenant's first request = %+v", got)
	}
	if got := svc.MayAsk(first, ip); !got.Decided() || got.Allowed {
		t.Fatalf("first tenant's exhausted quota = %+v", got)
	}
	if err := db.Run(second, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var visible int64
		if err := tx.DB().Table("platformkit_limits").Count(&visible).Error; err != nil {
			return err
		}
		if visible != 0 {
			t.Errorf("second tenant read %d protected counter rows", visible)
		}
		changed := tx.DB().Exec("UPDATE platformkit_limits SET count = 0")
		if changed.RowsAffected != 0 {
			t.Errorf("second tenant changed %d protected counter rows", changed.RowsAffected)
		}
		return changed.Error
	}); err != nil {
		t.Fatal(err)
	}
	if got := svc.MayAsk(second, ip); !got.Decided() || !got.Allowed {
		t.Fatalf("same address in second tenant spent the first tenant's quota: %+v", got)
	}
	if _, err := admin.ExecContext(t.Context(), "UPDATE platformkit_limits SET window_start = now() - interval '30 minutes'"); err != nil {
		t.Fatal(err)
	}
	if got := svc.MayAsk(first, ip); !got.Decided() || got.Allowed || got.Policy.Window != time.Hour {
		t.Fatalf("configured hour reset after the default fifteen minutes: %+v", got)
	}
	if _, err := admin.ExecContext(t.Context(), "UPDATE platformkit_limits SET window_start = now() - interval '61 minutes'"); err != nil {
		t.Fatal(err)
	}
	if got := svc.MayAsk(first, ip); !got.Decided() || !got.Allowed || got.Remaining != 0 {
		t.Fatalf("expired configured window did not start a fresh quota: %+v", got)
	}
}
