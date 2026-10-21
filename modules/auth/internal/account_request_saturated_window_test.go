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

func TestASaturatedAccountCounterRestartsOnlyAfterItsConfiguredWindow(t *testing.T) {
	admin, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, true, func(d *auth.Deps) {
		d.AccountRequests = contracts.AccountRequestBudget{Quota: 2, Window: time.Hour}
	})
	addr := from("203.0.113.84")
	const body = `{"email":"uncreated@example.com"}`
	if got := call(t, router, http.MethodPost, "/api/v1/public/auth/password/forgot", body, addr); got.Code != http.StatusOK {
		t.Fatalf("opening the counter = %d: %s", got.Code, got.Body)
	}
	effects := func() (n int) {
		if err := admin.QueryRowContext(t.Context(), "SELECT (SELECT count(*) FROM platformkit_outbox) + (SELECT count(*) FROM users) + (SELECT count(*) FROM password_tokens)").Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	before := effects()
	// Advance the route's isolated counter instead of sending billions of requests.
	changed, err := admin.ExecContext(t.Context(), "UPDATE platformkit_limits SET count = $1, window_start = now() - interval '30 minutes'", math.MaxInt32-1)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := changed.RowsAffected(); err != nil || n != 1 {
		t.Fatalf("advance the route's counter: rows=%d, error=%v", n, err)
	}
	for range 3 {
		got := call(t, router, http.MethodPost, "/api/v1/public/auth/register", body, addr)
		if got.Code != http.StatusTooManyRequests || !strings.Contains(got.Body.String(), "too many account requests") {
			t.Errorf("saturated counter admitted registration: status=%d body=%s", got.Code, got.Body)
		}
		if got.Header().Get(httpx.RateLimitPolicyHeader) != `"auth-account-requests";q=2;w=3600` ||
			!strings.HasPrefix(got.Header().Get(httpx.RateLimitHeader), `"auth-account-requests";r=0;t=`) {
			t.Errorf("saturated counter lost its policy: %v", got.Header())
		}
		if seconds, err := strconv.Atoi(got.Header().Get(httpx.RetryAfterHeader)); err != nil || seconds < 1 || seconds > 1800 {
			t.Errorf("refusal extended the original window: Retry-After=%q", got.Header().Get(httpx.RetryAfterHeader))
		}
		if len(got.Result().Cookies()) != 0 || strings.Contains(got.Body.String(), "uncreated@example.com") {
			t.Error("refused registration returned a session or account data")
		}
	}
	if after := effects(); after != before || len(mailbox.Sent()) != 0 {
		t.Errorf("refused registrations added %d rows/events and %d mails", after-before, len(mailbox.Sent()))
	}
	if _, err := admin.ExecContext(t.Context(), "UPDATE platformkit_limits SET window_start = now() - interval '61 minutes'"); err != nil {
		t.Fatal(err)
	}
	for attempt, want := range []int{http.StatusOK, http.StatusOK, http.StatusTooManyRequests} {
		got := call(t, router, http.MethodPost, "/api/v1/public/auth/password/forgot", body, addr)
		if got.Code != want {
			t.Errorf("fresh window request %d = %d, want %d: %s", attempt+1, got.Code, want, got.Body)
		}
	}
}
