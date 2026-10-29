package billing_test

// the_price_lists_write_closure_refuses_a_customer_who_holds_the_permission_test.go
// asks the door a generated screen calls — the registered resource's write
// closure — as a customer's administrator who holds every permission in their own
// tenant, and requires the answer to be the guard's own refusal.
//
// The price list stands on the operator's surface, so that one declaration is
// asked twice: by the request authorizer on the way into the route, and by the
// resource inside each write closure. The route half is held by
// TestThePriceListIsTheOperators, which addresses the customer's own host and
// reads 404 there. The closure half was held only by an assertion that any error
// satisfied — and a closure the guard should have stopped answers a call made
// outside a request transaction with an outage, 503, which is an error too. So
// one copy of that declaration could go and the only production mount that asks
// this door would still pass, noticing nothing.
//
// This case therefore reads the refusal as a status and a code: 403 carrying
// AUTH_NOT_OPERATOR, which is the sentence the operator boundary speaks and an
// outage cannot produce. It asks the same closure through the read declaration
// too, because the pair is one declaration read two ways. And it then writes the
// catalogue through the operator's own route and reads the plan back, so the case
// cannot pass on a mount that stopped taking writes for everybody.

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// TestThePriceListsWriteClosureRefusesACustomerWhoHoldsThePermission holds the
// price list's write closure — what a drawn form calls, under the guard of its
// own route rather than the resource's — to the operator declaration the module
// mounts it with: a customer's administrator is refused there with 403 naming the
// operator boundary, and the installation who owns the catalogue is taken through
// the same mount's route, once, with the plan it sent.
func TestThePriceListsWriteClosureRefusesACustomerWhoHoldsThePermission(t *testing.T) {
	api, router := mounted(t)
	var list *httpx.Resource
	for _, r := range api.Resources() {
		if r.Module == "billing" && r.Entity == "plan" {
			list = &r
		}
	}
	if list == nil {
		t.Fatal("the plan resource was never registered: there would be no screen writing over it")
	}
	// The authorizer this module is mounted with says yes to everything, so a
	// customer's administrator here holds the wildcard: what may refuse them is
	// the surface the write was declared on, and nothing else.
	holding := func(who tenancy.Tenant) context.Context {
		return tenancy.WithPrincipal(tenancy.WithTenant(t.Context(), who),
			tenancy.Principal{UserID: uuid.New(), Roles: []string{"admin"}})
	}

	_, err := list.Create(holding(customer), map[string]any{
		"code": "free", "name": "Free", "priceCents": 0, "currency": "EUR", "active": true,
	})
	if err == nil {
		t.Error("the price list's own write closure took a plan from a customer's administrator: the catalogue is the installation's, and the screen writes through this closure")
	} else {
		refused, ok := errors.AsType[*problem.Problem](err)
		if !ok {
			t.Errorf("the price list's write closure answered a customer's administrator with %v: the door answers with a problem a client can show", err)
		} else if refused.Status != http.StatusForbidden || !strings.Contains(refused.Detail, httpx.CodeNotOperator) {
			t.Errorf("the price list's write closure answered a customer's administrator with %d %q: the refusal this door owes names the operator boundary (%s), and an outage or a bare denial is what a closure that was never reached answers with", refused.Status, refused.Detail, httpx.CodeNotOperator)
		}
	}
	if list.Writable(holding(customer)) {
		t.Error("the price list declared itself writable to a customer's administrator: the same declaration the refused closure above answers from")
	}

	// The control: the same catalogue, the same mount, the caller the declaration
	// was written for. Without it the assertions above pass on an installation
	// that stopped serving the price list at all.
	code, out := call(t, router, http.MethodPost, writes,
		`{"code":"pro","name":"Pro","priceCents":2900,"currency":"EUR","active":true}`)
	if code != http.StatusCreated {
		t.Fatalf("the operator creating a plan through its own route = %d %s, want 201", code, out)
	}
	if code, out := call(t, router, http.MethodGet, plans, ""); code != http.StatusOK || !strings.Contains(out, `"code":"pro"`) {
		t.Errorf("reading back the catalogue = %d %s, want the plan the operator just wrote", code, out)
	}
}
