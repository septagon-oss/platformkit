package internal_test

import (
	"math"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/user"
)

func TestAnAcceptedAccountBudgetRefusesAfterItsIntegerSizedQuota(t *testing.T) {
	budget := contracts.AccountRequestBudget{Quota: math.MaxInt32, Window: time.Hour}
	if err := budget.Check(); err != nil {
		t.Logf("unsupported quota refused before construction: %v", err)
		return
	}
	admin, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, true, func(d *auth.Deps) {
		d.AccountRequests = budget
	})
	addr := from("203.0.113.81")
	first := call(t, router, http.MethodPost, "/api/v1/public/auth/password/forgot", `{"email":"unknown@example.com"}`, addr)
	if first.Code != http.StatusOK {
		t.Fatalf("opening the configured counter = %d: %s", first.Code, first.Body)
	}
	// Advance this isolated counter to the last available request without sending
	// billions of HTTP requests. Its window and key are those the real route wrote.
	changed, err := admin.ExecContext(t.Context(), "UPDATE platformkit_limits SET count = $1", budget.Quota-1)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := changed.RowsAffected(); err != nil || n != 1 {
		t.Fatalf("advance the route's one counter: rows=%d, error=%v", n, err)
	}
	last := call(t, router, http.MethodPost, "/api/v1/public/auth/password/forgot", `{"email":"unknown@example.com"}`, addr)
	if last.Code != http.StatusOK {
		t.Fatalf("last allowed request = %d: %s", last.Code, last.Body)
	}
	policy := `"auth-account-requests";q=` + strconv.Itoa(budget.Quota) + ";w=3600"
	if got := last.Header().Get(httpx.RateLimitPolicyHeader); got != policy {
		t.Fatalf("last request's policy = %q, want %q", got, policy)
	}
	var before int
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM platformkit_outbox").Scan(&before); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		got := call(t, router, http.MethodPost, "/api/v1/public/auth/register", `{"email":"over-budget@example.com"}`, addr)
		if got.Code != http.StatusTooManyRequests {
			t.Errorf("request beyond quota = %d, want 429: %s", got.Code, got.Body)
		}
		if got.Header().Get(httpx.RateLimitPolicyHeader) != policy || !strings.HasPrefix(got.Header().Get(httpx.RateLimitHeader), `"auth-account-requests";r=0;t=`) {
			t.Errorf("exhausted budget lost its headers: policy=%q rate=%q", got.Header().Get(httpx.RateLimitPolicyHeader), got.Header().Get(httpx.RateLimitHeader))
		}
	}
	var after int
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM platformkit_outbox").Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Errorf("requests beyond quota published %d events; want none", after-before)
	}
}
