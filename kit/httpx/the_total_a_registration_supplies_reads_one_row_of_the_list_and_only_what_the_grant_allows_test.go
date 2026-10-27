package httpx_test

// the_total_a_registration_supplies_reads_one_row_of_the_list_and_only_what_the_grant_allows_test.go
// asks the two facts a caller cannot see about the total registration *writes* for a
// resource that lists and names no Count of its own: which window it hands that list,
// and whose answer decides who may have the number.
//
// The window is what a card costs the tenant that reads it. The fallback exists because
// a custom loader computes its own total, so the count needs the loader to run and needs
// no row of it: `crud.Query{Limit: 1}`. A caller that asks for nothing gets a screenful —
// `crud.DefaultLimit` is 50 — so a fallback written as `crud.Query{}` would turn every
// dashboard card over such a loader into a read of fifty rows whose only used fact is a
// number. The door a module wrote is asked this question in kit/rest, where the count is a
// `COUNT(*)` and no page exists to read; the fallback is the same promise on the door the
// kernel wrote, and no caller of it can see the window from outside.
//
// The grant is the other half. This door is not only the operator's: a resource that
// declares an ordinary read permission has its total answered by the Authorizer, and the
// caller who does not hold it must be refused before the list runs. The refusal is what
// makes both halves worth pinning — a total handed to a caller its own list refuses is the
// leak, and a total asked for a screenful is the same door reading rows it then throws away.

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

// TestTheTotalASuppliedCountReadsOneRowAndRefusesTheCallerItsGrantRefuses registers a
// resource that lists, names no Count and declares an ordinary read permission, then asks
// the total registration supplies for it as a member of the tenant the authorizer turns
// away and as the same member once the authorizer grants. The refusal owes the denial and
// the permission, nothing run and the authorizer asked once; the answer owes the list's own
// total and a window of one row, never the screenful a caller that asked for nothing gets.
func TestTheTotalASuppliedCountReadsOneRowAndRefusesTheCallerItsGrantRefuses(t *testing.T) {
	api, _, f := setup(t)

	const readPerm = "billing:catalog"
	var (
		runs   int
		window crud.Query
	)
	api.RegisterResource(httpx.Resource{
		Module: "billing", Entity: "plan",
		Read: readPerm,
		List: func(_ context.Context, q crud.Query) ([]map[string]any, int64, error) {
			runs++
			window = q
			return []map[string]any{{"code": "pro"}}, 7, nil
		},
	})
	door := httpx.Resource{}
	for _, r := range api.Resources() {
		if r.Module == "billing" && r.Entity == "plan" {
			door = r
		}
	}
	if door.Count == nil {
		t.Fatal("registration supplied no Count for a resource that lists: the card that reads this collection has no number to show")
	}
	ctx := tenancy.WithPrincipal(tenancy.WithTenant(t.Context(), f.tenant),
		tenancy.Principal{UserID: uuid.New(), Roles: []string{"member"}})

	// A member of the right tenant holding the wrong permission: the total is tenant
	// work the roles table decides, so the door asks and the denial is the answer.
	f.asked.Store(0)
	total, err := door.Count(ctx)
	var refused *problem.Problem
	if !errors.As(err, &refused) {
		t.Fatalf("the supplied Count answered a member who does not hold %s with %d rows and %v: the total is read through the list the caller may not open, which ran %d time(s)", readPerm, total, err, runs)
	}
	if refused.Status != http.StatusForbidden || !strings.Contains(refused.Detail, httpx.CodeDenied) || !strings.Contains(refused.Detail, readPerm) {
		t.Errorf("the supplied Count answered a member who does not hold %s with %d %q: the refusal owes the denial (%s) and the permission this resource declares (%s)", readPerm, refused.Status, refused.Detail, httpx.CodeDenied, readPerm)
	}
	if total != 0 {
		t.Errorf("the refused Count returned %d, want no total: a caller turned away is not given the number behind the door", total)
	}
	if runs != 0 {
		t.Errorf("the refused Count read the list %d time(s), want never: a denial that arrives after the read has already spent the work it refuses", runs)
	}
	if got := f.asked.Load(); got != 1 {
		t.Errorf("the refused Count asked the authorizer %d time(s), want once: this declaration names a permission, so the denial has to come from the thing that decides permissions", got)
	}

	// The same caller once the grant is held: the answer is the total the list carries,
	// not the length of the page it was asked to read, and the window is one row because
	// nothing here reads a row.
	f.allow = true
	runs, window = 0, crud.Query{}
	f.asked.Store(0)
	total, err = door.Count(ctx)
	if err != nil {
		t.Fatalf("the supplied Count refused a member who holds the permission: %v", err)
	}
	if total != 7 {
		t.Errorf("the supplied Count answered %d, want the total its list carries (7): a count of a page would answer one, which is the row it loaded, not the collection", total)
	}
	if runs != 1 {
		t.Errorf("the supplied Count read the list %d time(s), want once", runs)
	}
	if window.Limit != 1 {
		t.Errorf("the supplied Count asked its list for a window of %d rows, want exactly 1: %d is what a caller that asks for nothing gets, and a total needs no row of the page it counts", window.Limit, crud.DefaultLimit)
	}
	if window.Offset != 0 {
		t.Errorf("the supplied Count asked its list at offset %d, want 0: a total read from somewhere inside the collection is a different number", window.Offset)
	}
	if got := f.asked.Load(); got != 1 {
		t.Errorf("the Count asked the authorizer %d time(s), want once: the door asks its grant once and reads once", got)
	}
}
