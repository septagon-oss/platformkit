package httpx_test

// the_in_process_write_door_of_an_operators_resource_names_the_operator_boundary_test.go
// asks the kernel's own copy of the operator boundary: the one a resource's write
// closure makes, in process, beneath whatever route the caller arrived at.
//
// A module declares that boundary once — a Write permission carried by
// OperatorWrite — and the kernel asks it twice: by the request middleware on the way
// into the route, and by each closure of the registered resource. The cases that
// address an operator route hold the first copy; none of them notices when the
// second goes, and the closures are the doors a drawn form, a dashboard card and the
// catalogue call. A customer's administrator holds the wildcard in their own tenant,
// so once the closure stops asking, the roles table answers that write yes by
// construction, the address still refuses the same caller, and the first visible sign
// is a row nobody was meant to write.
//
// The status is read rather than merely expected, because a closure whose guard is
// gone does not stay silent: it runs its body outside the request transaction and
// answers an outage, which is an error too. The counters say what the status alone
// cannot — that the refused write ran nothing and asked the authorizer nothing — and
// the installation's own call is the control that says the door still opens.

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

// TestTheInProcessWriteDoorOfAnOperatorsResourceNamesTheOperatorBoundary mounts one
// resource whose writes carry OperatorWrite and asks its Create closure as each of
// the two tenants, with an authorizer that grants everything so that only the
// declared surface can answer. A customer's administrator is refused with 403 whose
// detail names the operator boundary (AUTH_NOT_OPERATOR), having run neither the
// closure body nor a question to the authorizer, and declares the door not writable
// to them; the installation who owns the catalogue is taken through the same
// closure, once.
func TestTheInProcessWriteDoorOfAnOperatorsResourceNamesTheOperatorBoundary(t *testing.T) {
	api, _, f := setup(t)
	f.allow = true

	writes := 0
	api.RegisterResource(httpx.Resource{
		Module: "billing", Entity: "plan",
		Write: "billing:catalog", OperatorWrite: true,
		Create: func(context.Context, map[string]any) (map[string]any, error) {
			writes++
			return map[string]any{"code": "pro"}, nil
		},
	})
	door := httpx.Resource{}
	for _, r := range api.Resources() {
		if r.Module == "billing" && r.Entity == "plan" {
			door = r
		}
	}
	if door.Entity != "plan" {
		t.Fatal("the API recorded no plan resource: one that was never registered has no closure to ask")
	}
	// The strongest caller a customer has: an administrator whose roles answer
	// every permission in their own tenant, which is why the boundary has to be
	// asked before the roles table is.
	in := func(tenant tenancy.Tenant) context.Context {
		ctx := tenancy.WithTenant(t.Context(), tenant)
		return tenancy.WithPrincipal(ctx, tenancy.Principal{UserID: uuid.New(), Roles: []string{"admin"}})
	}

	// The customer's tenant: the fixture's tenant carries no operator flag, so
	// this is the caller the declaration was written against.
	customer := in(f.tenant)
	f.asked.Store(0)
	_, err := door.Create(customer, map[string]any{"code": "pro"})
	if err == nil {
		t.Errorf("the write closure of a resource declared for the operator took a plan from a customer's administrator after %d runs of its body: the price list is the installation's, and a generated form writes through this closure", writes)
	} else {
		var refused *problem.Problem
		if !errors.As(err, &refused) {
			t.Errorf("the write closure of a resource declared for the operator answered a customer's administrator with %v: the door answers with a problem a client can show", err)
		} else if refused.Status != http.StatusForbidden || !strings.Contains(refused.Detail, httpx.CodeNotOperator) {
			t.Errorf("the write closure of a resource declared for the operator answered a customer's administrator with %d %q: the refusal this door owes names the operator boundary (%s), which an outage or a bare denial cannot", refused.Status, refused.Detail, httpx.CodeNotOperator)
		}
	}
	if writes != 0 {
		t.Errorf("the refused write ran the closure body %d time(s), want never", writes)
	}
	if got := f.asked.Load(); got != 0 {
		t.Errorf("the refused write asked the authorizer %d time(s), want never: the tenant flag decides this before the roles table, whose answer for a customer's administrator is always yes", got)
	}
	if door.Writable(customer) {
		t.Error("the resource declared itself writable to a customer's administrator: the same declaration the refused closure above answers from")
	}

	// The control: the same closure, the same mount, the tenant the declaration
	// names. Without it the assertions above pass on a door that stopped opening
	// for anybody, and the operator's own call is what a customer's refusal is
	// refused against.
	f.tenant.Operator = true
	installation := in(f.tenant)
	if !door.Writable(installation) {
		t.Error("the resource declared itself not writable to the installation whose permission it carries")
	}
	f.asked.Store(0)
	writes = 0
	if _, err := door.Create(installation, map[string]any{"code": "free"}); err != nil {
		t.Errorf("the write closure of a resource declared for the operator refused the installation: %v", err)
	}
	if writes != 1 {
		t.Errorf("the installation's write ran the closure body %d time(s), want once", writes)
	}
	if got := f.asked.Load(); got != 1 {
		t.Errorf("the installation's write asked the authorizer %d time(s), want once: past the boundary the grant is asked like any other", got)
	}
}
