package internal_test

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/limit"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/user"
)

// A configured budget of twelve a minute admits twelve forgotten-password asks
// from one address and refuses the thirteenth with the same 429 problem and
// headers as the default's eleventh. Twelve is over ten, so the eleventh being
// admitted is the line that fails when the budget is ignored.
func TestAConfiguredAccountRequestBudgetAdmitsItsQuotaAndRefusesTheNext(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, func(d *auth.Deps) {
		d.AccountRequests = contracts.AccountRequestBudget{Quota: 12, Window: time.Minute}
	})
	askUntilRefused(t, router, 12, time.Minute)
}

// An unset budget keeps ten per fifteen minutes, written as literals so a moved
// default fails here even though the neighbouring cases read the constant.
func TestAnUnsetAccountRequestBudgetKeepsTenPerFifteenMinutes(t *testing.T) {
	router, _, _ := mount(t, auth.OIDC{})
	askUntilRefused(t, router, 10, 15*time.Minute)
}

// The budget is one counter across the routes that spend it: with two a minute,
// one forgotten-password ask and one registration spend it, and a third ask on
// either route is refused under the one policy name.
func TestAConfiguredBudgetIsOneCounterAcrossTheRoutesThatSpendIt(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, true, func(d *auth.Deps) {
		d.AccountRequests = contracts.AccountRequestBudget{Quota: 2, Window: time.Minute}
	})
	addr := from("203.0.113.41")
	if got := call(t, router, http.MethodPost, "/api/v1/public/auth/password/forgot", `{"email":"unknown@acme.localhost"}`, addr); got.Code != http.StatusOK {
		t.Fatalf("the first ask = %d, want 200: %s", got.Code, got.Body)
	}
	if got := call(t, router, http.MethodPost, "/api/v1/public/auth/register", `{"email":"student@acme.localhost"}`, addr); got.Code != http.StatusAccepted {
		t.Fatalf("the registration = %d, want 202: %s", got.Code, got.Body)
	}
	for _, route := range []string{"/api/v1/public/auth/register", "/api/v1/public/auth/password/forgot"} {
		got := call(t, router, http.MethodPost, route, `{"email":"third@acme.localhost"}`, addr)
		if got.Code != http.StatusTooManyRequests {
			t.Errorf("the third ask on %s = %d, want 429 under the one counter", route, got.Code)
		}
		if rate := got.Header().Get(httpx.RateLimitHeader); !strings.HasPrefix(rate, `"`+contracts.AccountRequestsPolicyName+`";r=0;`) {
			t.Errorf("the third ask on %s stated RateLimit=%q, want %s with nothing left", route, rate, contracts.AccountRequestsPolicyName)
		}
	}
}

// The largest budget Check accepts is still refused past its quota, again and
// again, with its headers and no event: the counter stops at its ceiling rather
// than failing open. The route's one counter is advanced to the last ask by hand.
func TestTheLargestAccountRequestBudgetKeepsRefusingPastItsQuota(t *testing.T) {
	budget := contracts.AccountRequestBudget{Quota: limit.MaxQuota, Window: time.Hour}
	if err := budget.Check(); err != nil {
		t.Fatalf("Check(%+v) = %v; the ceiling is a budget a deployment may name", budget, err)
	}
	admin, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, true, func(d *auth.Deps) { d.AccountRequests = budget })
	addr := from("203.0.113.42")
	ask := func(route, body string) *httptest.ResponseRecorder {
		return call(t, router, http.MethodPost, route, body, addr)
	}
	if got := ask("/api/v1/public/auth/password/forgot", `{"email":"unknown@acme.localhost"}`); got.Code != http.StatusOK {
		t.Fatalf("the first ask = %d: %s", got.Code, got.Body)
	}
	if _, err := admin.ExecContext(t.Context(), "UPDATE platformkit_limits SET count = $1", budget.Quota-1); err != nil {
		t.Fatal(err)
	}
	if got := ask("/api/v1/public/auth/password/forgot", `{"email":"unknown@acme.localhost"}`); got.Code != http.StatusOK {
		t.Fatalf("the last ask inside the quota = %d: %s", got.Code, got.Body)
	}
	events := func() (n int) {
		if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM platformkit_outbox").Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	before := events()
	policy := `"` + contracts.AccountRequestsPolicyName + `";q=` + strconv.Itoa(limit.MaxQuota) + ";w=3600"
	for i := range 3 {
		got := ask("/api/v1/public/auth/register", `{"email":"over@acme.localhost"}`)
		rate := got.Header().Get(httpx.RateLimitHeader)
		if got.Code != http.StatusTooManyRequests || got.Header().Get(httpx.RateLimitPolicyHeader) != policy ||
			!strings.HasPrefix(rate, `"`+contracts.AccountRequestsPolicyName+`";r=0;t=`) {
			t.Errorf("registration %d past the quota = %d, RateLimit=%q; want 429 under %s with nothing left", i+1, got.Code, rate, policy)
		}
	}
	if after := events(); after != before {
		t.Errorf("registrations past the quota published %d events; want none", after-before)
	}
}

// askUntilRefused makes quota forgotten-password asks from one address, each
// stating the configured policy and the quota left, and a last one that answers
// 429 with Retry-After inside the window.
func askUntilRefused(t *testing.T, router http.Handler, quota int, window time.Duration) {
	t.Helper()
	seconds := int(window.Seconds())
	policy := `"` + contracts.AccountRequestsPolicyName + `";q=` + strconv.Itoa(quota) + ";w=" + strconv.Itoa(seconds)
	var refused *httptest.ResponseRecorder
	for attempt := range quota + 1 {
		got := call(t, router, http.MethodPost, "/api/v1/public/auth/password/forgot", `{"email":"unknown@acme.localhost"}`, from("203.0.113.40"))
		if items := got.Header().Values(httpx.RateLimitPolicyHeader); len(items) != 1 || items[0] != policy {
			t.Errorf("attempt %d: RateLimit-Policy=%q, want this one item %q", attempt+1, items, policy)
		}
		left := max(quota-attempt-1, 0)
		if rate, want := got.Header().Get(httpx.RateLimitHeader), `"`+contracts.AccountRequestsPolicyName+`";r=`+strconv.Itoa(left)+";t="; !strings.HasPrefix(rate, want) {
			t.Errorf("attempt %d: RateLimit=%q, want it to begin %q", attempt+1, rate, want)
		}
		want := http.StatusOK
		if attempt == quota {
			want, refused = http.StatusTooManyRequests, got
		}
		if got.Code != want {
			t.Fatalf("attempt %d = %d, want %d: %s", attempt+1, got.Code, want, got.Body)
		}
	}
	if wait, err := strconv.Atoi(refused.Header().Get(httpx.RetryAfterHeader)); err != nil || wait <= 0 || wait > seconds {
		t.Errorf("the refused attempt answered Retry-After=%q, want a wait inside the %s window", refused.Header().Get(httpx.RetryAfterHeader), window)
	}
	if body := refused.Body.String(); !strings.Contains(body, "too many reset requests from this address") {
		t.Errorf("the refusal's body is %s, want the route's own sentence", body)
	}
}
