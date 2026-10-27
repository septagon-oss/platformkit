package httpx_test

// the_count_a_registration_supplies_for_a_resource_that_only_lists_answers_its_read_declaration_test.go
// asks the one door registration *writes* rather than wraps: the total it hands to a
// resource that lists rows and names no Count of its own.
//
// `Resource.Count` promises two things at once — "Registration guards it with Read and
// supplies a List-based fallback for resources with custom loading" — and the second is
// the one no case reads. A module that mounts its own routes and registers the entity for
// the catalogue and the screens has no reason to write a count beside its list, so it is
// the fallback that reaches a card and a screen: the kernel built that door, so the kernel
// owes it the guard the declaration states. The doors a module wrote are asked elsewhere
// in this package; a door nobody wrote is the one an edit to the wrapping could leave bare.
//
// What makes the refusal, and not only the number, the thing to pin: a fallback wired to
// the list but asked of nothing would answer a caller its own list refuses, and both
// doors read the same rows. So this case asks the supplied count as a customer's
// administrator — owed the operator boundary, naming the read permission, with the list
// it would have read run zero times — and as the installation, whose answer is the total
// and one question to the authorizer.

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// TestTheCountASuppliedFallbackAnswersTheReadDeclarationOfTheListItReads registers a
// resource that lists and names no Count, asks the count registration supplies for it as a
// customer's administrator and then as the installation, and holds the first answer to the
// resource's own read declaration with its list unrun and the authorizer unasked.
func TestTheCountASuppliedFallbackAnswersTheReadDeclarationOfTheListItReads(t *testing.T) {
	api, _, f := setup(t)
	f.allow = true

	const readPerm = "billing:catalog"
	runs := map[string]int{}
	api.RegisterResource(httpx.Resource{
		Module: "billing", Entity: "plan",
		Read: readPerm, OperatorRead: true,
		List: func(_ context.Context, _ crud.Query) ([]map[string]any, int64, error) {
			runs["List"]++
			return []map[string]any{{"code": "pro"}}, 7, nil
		},
	})
	door := httpx.Resource{}
	for _, r := range api.Resources() {
		if r.Module == "billing" && r.Entity == "plan" {
			door = r
		}
	}
	if door.Entity != "plan" {
		t.Fatal("the API recorded no plan resource, so there is no door here to ask")
	}
	// The promise the fallback exists to keep: a resource that names no Count is not a
	// card with no number and not a nil field a screen panics on.
	if door.Count == nil {
		t.Fatal("registration supplied no Count for a resource that lists: the total is owed to whoever reads the collection, not only to whoever wrote one")
	}

	in := func(tenant tenancy.Tenant) context.Context {
		ctx := tenancy.WithTenant(t.Context(), tenant)
		return tenancy.WithPrincipal(ctx, tenancy.Principal{UserID: uuid.New(), Roles: []string{"admin"}})
	}

	// A customer's administrator holds every grant in their own tenant, which is why
	// reads declared for the operator are refused before the roles table is asked.
	f.asked.Store(0)
	total, err := door.Count(in(f.tenant))
	var refused *problem.Problem
	if !errors.As(err, &refused) {
		t.Fatalf("the supplied Count of a resource declared for the operator answered a customer's administrator with %d rows and %v: the door registration wrote answers to the resource's own read declaration, and the %d run(s) of its list mean it opened", total, err, runs["List"])
	}
	if refused.Status != http.StatusForbidden || !strings.Contains(refused.Detail, httpx.CodeNotOperator) || !strings.Contains(refused.Detail, readPerm) {
		t.Errorf("the supplied Count answered a customer's administrator with %d %q: the refusal owes the operator boundary (%s) and the permission this resource declares (%s), the same two facts its List answers with", refused.Status, refused.Detail, httpx.CodeNotOperator, readPerm)
	}
	if total != 0 {
		t.Errorf("the refused Count returned %d, want no total: a caller turned away at the door is not given the number behind it", total)
	}
	if runs["List"] != 0 {
		t.Errorf("the refused Count ran the list %d time(s), want never: the fallback reads through that closure, so the row read is the refusal's own contradiction", runs["List"])
	}
	if got := f.asked.Load(); got != 0 {
		t.Errorf("the refused Count asked the authorizer %d time(s), want never: the operator boundary decides before the roles table, whose answer for this caller is always yes", got)
	}

	// The control, without which every assertion above holds on a door that opened for
	// nobody: the installation the declaration names is answered by the same fallback,
	// through the list it reads, and asks the authorizer for itself.
	f.tenant.Operator = true
	f.asked.Store(0)
	total, err = door.Count(in(f.tenant))
	if err != nil {
		t.Errorf("the supplied Count refused the installation whose read permission the resource carries: %v", err)
	}
	if total != 7 {
		t.Errorf("the supplied Count answered %d, want the total its own list carries (7): the fallback is a count of that list and nothing else", total)
	}
	if runs["List"] != 1 {
		t.Errorf("the installation's Count ran the list %d time(s), want once", runs["List"])
	}
	if got := f.asked.Load(); got != 1 {
		t.Errorf("the installation's Count asked the authorizer %d time(s), want once: past the boundary the door asks its own grant", got)
	}
}
