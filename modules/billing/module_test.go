package billing_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

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

var acme = tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}

// caller is the two answers httpx.New needs, both of them yes.
type caller struct{}

func (caller) ByHost(_ context.Context, _ db.Tx[db.System], h string) (tenancy.Tenant, error) {
	if h != host {
		return tenancy.Tenant{}, tenancy.ErrNoSuchHost
	}
	return acme, nil
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
	req := httptest.NewRequest(method, "http://"+host+at, strings.NewReader(body))
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
