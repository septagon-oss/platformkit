package billing_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/billing"
	"github.com/septagon-oss/platformkit/modules/billing/contracts"
)

const (
	host  = "acme.test"
	plans = "/api/v1/billing/plans"
	sub   = "/api/v1/billing/subscription"
)

// acme is the operator's own tenant, and customer is somebody else's. The two
// exist because the price list is the operator's: writing a plan is refused at
// any host but the operator's, by the kernel, before the authorizer below is
// asked anything.
var (
	acme     = tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme", Operator: true}
	customer = tenancy.Tenant{ID: uuid.New(), Slug: "widgets", Name: "Widgets"}
)

const customerHost = "widgets.test"

// caller is the two answers httpx.New needs, and the authorizer says yes to
// everything — which is the point of the operator test below: a customer's
// administrator legitimately holds the wildcard in their own tenant, and it is
// the kernel and not the roles table that has to refuse them the catalogue.
type caller struct{}

func (caller) ByHost(_ context.Context, _ db.Tx[db.System], h string) (tenancy.Tenant, error) {
	switch h {
	case host:
		return acme, nil
	case customerHost:
		return customer, nil
	}
	return tenancy.Tenant{}, tenancy.ErrNoSuchHost
}
func (caller) Allowed(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) { return true, nil }

// mounted is the module as main mounts it: the manifest's own Routes, on the
// real API, against a real Postgres.
func mounted(t *testing.T) (*httpx.API, chi.Router) {
	t.Helper()
	_, conn := dbtest.Schema(t)
	api, router := httpx.New(httpx.Options{
		PublicHost: host, Tenants: caller{}, Conn: conn, Authorize: caller{},
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: uuid.New()}, true, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	billing.Module(billing.Deps{Payments: billing.Manual()}).Routes(api)
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the mounted routes do not declare themselves: %v", err)
	}
	return api, router
}

func call(t *testing.T, r http.Handler, method, at, body string) (int, string) {
	t.Helper()
	return callAt(t, r, host, method, at, body)
}

// callAt is the same request at a named host, for the two tenants above.
func callAt(t *testing.T, r http.Handler, h, method, at, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, "http://"+h+at, strings.NewReader(body))
	req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code, w.Body.String()
}

// TestWhetherAPlanSellsIsTheCallersToSay is the trap the Active field's comment
// describes, tested rather than trusted. Neither layer below can tell a false
// from an absent bool, so the property is required: what a caller sends is what
// the row holds, both ways round, and a body that leaves it out is refused
// instead of guessed at.
func TestWhetherAPlanSellsIsTheCallersToSay(t *testing.T) {
	_, router := mounted(t)
	for _, tt := range []struct {
		body string
		want bool
	}{
		{`{"code":"pro","name":"Pro","priceCents":2900,"currency":"EUR","active":true}`, true},
		{`{"code":"legacy","name":"Legacy","priceCents":900,"currency":"EUR","active":false}`, false},
	} {
		code, body := call(t, router, http.MethodPost, plans, tt.body)
		if code != http.StatusCreated {
			t.Fatalf("POST %s = %d %s, want 201", plans, code, body)
		}
		if got := strings.Contains(body, `"active":true`); got != tt.want {
			t.Errorf("POST %s answered %s; want active=%v", tt.body, body, tt.want)
		}
	}
	code, body := call(t, router, http.MethodPost, plans, `{"code":"maybe","name":"Maybe","priceCents":100,"currency":"EUR"}`)
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, "active") {
		t.Errorf("POST a plan that says nothing about selling = %d %s, want 422 naming the property", code, body)
	}
}

// TestAPlanSomebodyIsOnCannotBeDeleted is the delete hook, and the reason the
// subscriptions table needs no foreign key: the refusal is about live
// subscriptions, which is a rule a foreign key cannot express.
func TestAPlanSomebodyIsOnCannotBeDeleted(t *testing.T) {
	_, router := mounted(t)
	code, body := call(t, router, http.MethodPost, plans, `{"code":"pro","name":"Pro","priceCents":0,"currency":"EUR","active":true}`)
	if code != http.StatusCreated {
		t.Fatalf("POST %s = %d %s, want 201", plans, code, body)
	}
	id := field(t, body, "id")

	// Nobody is on it yet.
	if code, body = call(t, router, http.MethodDelete, plans+"/"+id, ""); code != http.StatusNoContent {
		t.Fatalf("DELETE an unused plan = %d %s, want 204", code, body)
	}

	code, body = call(t, router, http.MethodPost, plans, `{"code":"team","name":"Team","priceCents":0,"currency":"EUR","active":true}`)
	if code != http.StatusCreated {
		t.Fatalf("POST %s = %d %s, want 201", plans, code, body)
	}
	id = field(t, body, "id")
	if code, body = call(t, router, http.MethodPost, sub+"/subscribe", `{"planId":"`+id+`"}`); code != http.StatusOK {
		t.Fatalf("subscribe = %d %s, want 200", code, body)
	}
	if code, body = call(t, router, http.MethodDelete, plans+"/"+id, ""); code != http.StatusConflict ||
		!strings.Contains(body, "deactivate it instead") {
		t.Errorf("DELETE a plan somebody is on = %d %s, want 409 saying what to do instead", code, body)
	}

	// And once they leave, it goes.
	if code, body = call(t, router, http.MethodPost, sub+"/cancel", `{}`); code != http.StatusOK {
		t.Fatalf("cancel = %d %s, want 200", code, body)
	}
	if code, body = call(t, router, http.MethodDelete, plans+"/"+id, ""); code != http.StatusNoContent {
		t.Errorf("DELETE a plan nobody is on any more = %d %s, want 204", code, body)
	}
}

// TestTheSubscriptionIsASingleton: there is no list and no id in the path, and
// a tenant that has never subscribed has nothing rather than an empty page.
func TestTheSubscriptionIsASingleton(t *testing.T) {
	_, router := mounted(t)
	if code, body := call(t, router, http.MethodGet, sub, ""); code != http.StatusNotFound {
		t.Errorf("GET %s before subscribing = %d %s, want 404", sub, code, body)
	}
	code, body := call(t, router, http.MethodPost, plans, `{"code":"pro","name":"Pro","priceCents":2900,"currency":"EUR","active":true}`)
	if code != http.StatusCreated {
		t.Fatalf("POST %s = %d %s, want 201", plans, code, body)
	}
	id := field(t, body, "id")
	if code, body = call(t, router, http.MethodPost, sub+"/subscribe", `{"planId":"`+id+`"}`); code != http.StatusOK ||
		!strings.Contains(body, `"`+contracts.StatusTrial+`"`) {
		t.Fatalf("subscribe = %d %s, want 200 and a trial", code, body)
	}
	if code, body = call(t, router, http.MethodGet, sub, ""); code != http.StatusOK ||
		!strings.Contains(body, `"planId":"`+id+`"`) {
		t.Errorf("GET %s = %d %s, want the subscription", sub, code, body)
	}
}

// TestEveryEventTheRoutesDeclareIsInTheManifest is the boot gate kit/app runs,
// against this module alone.
func TestEveryEventTheRoutesDeclareIsInTheManifest(t *testing.T) {
	api, _ := mounted(t)
	declared := api.Events()
	for _, want := range []string{contracts.EventSubscribed, contracts.EventCancelled} {
		if !slices.Contains(declared, want) {
			t.Errorf("the routes declare %v; %s is not among them", declared, want)
		}
	}
	for _, e := range declared {
		if !slices.Contains(contracts.Events, e) {
			t.Errorf("a route publishes %q and the manifest does not name it", e)
		}
	}
}

// TestAModuleWithNoPaymentProviderDoesNotCompose: a wiring mistake fails where
// it is written, not at two in the morning.
func TestAModuleWithNoPaymentProviderDoesNotCompose(t *testing.T) {
	defer func() {
		if r := recover(); r == nil || !strings.Contains(r.(string), "billing.Manual()") {
			t.Errorf("Module with no provider panicked with %v; it names the one to wire", r)
		}
	}()
	billing.Module(billing.Deps{})
}

func field(t *testing.T, body, name string) string {
	t.Helper()
	_, rest, ok := strings.Cut(body, `"`+name+`":"`)
	if !ok {
		t.Fatalf("no %s in %s", name, body)
	}
	out, _, _ := strings.Cut(rest, `"`)
	return out
}

// TestThePriceListIsTheOperators is the review's second critical finding, over
// HTTP and through the manifest's own routes.
//
// The authorizer above says yes to everything, which is the point: a customer's
// administrator legitimately holds the wildcard in their own tenant, and it is
// the kernel that has to refuse them the catalogue — before the roles table is
// consulted, because no wildcard satisfies an operator grant.
func TestThePriceListIsTheOperators(t *testing.T) {
	_, router := mounted(t)
	const body = `{"code":"pro","name":"Pro","priceCents":2900,"currency":"EUR","active":true}`

	// The operator writes it.
	code, out := call(t, router, http.MethodPost, plans, body)
	if code != http.StatusCreated {
		t.Fatalf("the operator creating a plan = %d %s, want 201", code, out)
	}
	id := field(t, out, "id")

	// A customer cannot, at any of the three write doors.
	for _, w := range []struct{ method, at, body string }{
		{http.MethodPost, plans, `{"code":"free","name":"Free","priceCents":0,"currency":"EUR","active":true}`},
		{http.MethodPatch, plans + "/" + id, `{"priceCents":0}`},
		{http.MethodDelete, plans + "/" + id, ""},
	} {
		code, out := callAt(t, router, customerHost, w.method, w.at, w.body)
		if code != http.StatusForbidden {
			t.Errorf("a customer's %s %s = %d %s, want 403", w.method, w.at, code, out)
		}
	}

	// And it reads the same catalogue the operator wrote, because a price list
	// nobody can see is a price list nobody can buy from.
	code, out = callAt(t, router, customerHost, http.MethodGet, plans, "")
	if code != http.StatusOK || !strings.Contains(out, `"code":"pro"`) {
		t.Errorf("a customer reading the catalogue = %d %s", code, out)
	}
}

// TestTheGeneratedScreenIsGuardedTheSameWay. The admin screens do not call the
// API: they hold the resource kit/rest registered and call its closures. So the
// resource has to carry the operator declaration too, or the form is a second
// door onto the price list that only asks for the permission every tenant's
// administrator already holds.
func TestTheGeneratedScreenIsGuardedTheSameWay(t *testing.T) {
	api, _ := mounted(t)
	var plan *httpx.Resource
	for _, r := range api.Resources() {
		if r.Module == "billing" && r.Entity == "plan" {
			plan = &r
		}
	}
	if plan == nil {
		t.Fatal("the plan resource was not registered; there would be no screen")
	}
	signedIn := func(who tenancy.Tenant) context.Context {
		return tenancy.WithPrincipal(tenancy.WithTenant(t.Context(), who),
			tenancy.Principal{UserID: uuid.New(), Roles: []string{"admin"}})
	}
	switch {
	case !plan.Writable(signedIn(acme)):
		t.Error("the operator cannot write its own price list through the screen")
	case plan.Writable(signedIn(customer)):
		t.Error("a customer's administrator can write the price list through the screen")
	case !plan.Readable(signedIn(customer)):
		t.Error("a customer cannot read the catalogue it is choosing from")
	}
	// And the same declaration a page would mount its write route with.
	if _, err := plan.Create(signedIn(customer), map[string]any{"code": "free"}); err == nil {
		t.Error("the resource's own create closure let a customer through")
	}
}

// TestAnAnniversaryDoesNotDriftPastAMonthEnd. AddDate normalises, so a monthly
// plan bought on the 31st of January used to advance to the 3rd of March and
// stay on the 3rd forever. Clamping fixed February and broke every month after
// it, because the next period was then computed from the clamped day; the
// anchor is what makes the 31st come back. The dates are the review's.
func TestAnAnniversaryDoesNotDriftPastAMonthEnd(t *testing.T) {
	const day = "2006-01-02"
	for _, tt := range []struct {
		from, interval string
		anchor         int
		want           string
	}{
		{"2026-01-31", contracts.IntervalMonth, 31, "2026-02-28"},
		{"2024-01-31", contracts.IntervalMonth, 31, "2024-02-29"}, // a leap year
		{"2026-03-31", contracts.IntervalMonth, 31, "2026-04-30"},
		{"2026-08-31", contracts.IntervalMonth, 31, "2026-09-30"},
		{"2024-02-29", contracts.IntervalYear, 29, "2025-02-28"},
		// The mutation this whole column exists for: the month after a clamp
		// returns to the anchor. Without an anchor the 28th of February
		// advanced to the 28th of March and the 31st was gone forever.
		{"2026-02-28", contracts.IntervalMonth, 31, "2026-03-31"},
		{"2026-02-28", contracts.IntervalMonth, 30, "2026-03-30"},
		{"2024-02-29", contracts.IntervalMonth, 31, "2024-03-31"},
		// An anchor of zero is the day of the date it is given, which is what
		// every row written before the column existed means.
		{"2026-01-31", contracts.IntervalMonth, 0, "2026-02-28"},
		// And the ordinary cases, which have to keep working: a date that fits
		// in the target month is that date, and a month end that is a month end
		// in both is unchanged.
		{"2026-01-15", contracts.IntervalMonth, 15, "2026-02-15"},
		{"2026-12-31", contracts.IntervalMonth, 31, "2027-01-31"},
		{"2026-01-31", contracts.IntervalYear, 31, "2027-01-31"},
	} {
		from, err := time.Parse(day, tt.from)
		if err != nil {
			t.Fatalf("parse %s: %v", tt.from, err)
		}
		if got := contracts.Advance(from, tt.interval, tt.anchor).Format(day); got != tt.want {
			t.Errorf("Advance(%s, %s, %d) = %s, want %s", tt.from, tt.interval, tt.anchor, got, tt.want)
		}
	}
	// The time of day survives the clamp: a period that ended at nine in the
	// morning ends at nine in the morning.
	from := time.Date(2026, 1, 31, 9, 30, 0, 0, time.UTC)
	if got := contracts.Advance(from, contracts.IntervalMonth, 31); got.Hour() != 9 || got.Minute() != 30 {
		t.Errorf("the clamp moved the time of day to %s", got)
	}
}

// TestAPlanIsWrittenInsideItsBounds: the four things a plan's Validate refuses
// that it used to accept.
func TestAPlanIsWrittenInsideItsBounds(t *testing.T) {
	for _, tt := range []struct {
		what string
		plan contracts.Plan
	}{
		{"a currency that is three letters and not a currency",
			contracts.Plan{Code: "pro", Name: "Pro", Currency: "ZZZ", Active: true}},
		{"a price larger than any invoice",
			contracts.Plan{Code: "pro", Name: "Pro", Currency: "EUR", PriceCents: contracts.MaxPriceCents + 1}},
		{"a code with spaces and punctuation in it",
			contracts.Plan{Code: "Pro Monthly!!", Name: "Pro", Currency: "EUR"}},
		{"a code of one character",
			contracts.Plan{Code: "p", Name: "Pro", Currency: "EUR"}},
	} {
		plan := tt.plan
		if err := plan.Validate(t.Context()); err == nil {
			t.Errorf("%s was accepted", tt.what)
		}
	}
	// And the one that has to pass, in every currency somebody actually sells in.
	for _, code := range []string{"EUR", "usd", "JPY", "GBP", "BRL"} {
		plan := contracts.Plan{Code: "pro-monthly", Name: "Pro", Currency: code, PriceCents: 2900, Active: true}
		if err := plan.Validate(t.Context()); err != nil {
			t.Errorf("a plan priced in %s: %v", code, err)
		}
	}
}
