package httpx_test

// the_total_registration_supplies_answers_a_failed_read_with_the_failure_rather_than_a_number_test.go
// asks the one answer of the generated total that a caller cannot reconstruct from
// the number it is given: what the door says when the list it reads through could
// not read.
//
// Registration writes a Count for every resource that lists and names none of its
// own, and the catalogue entry, the dashboard card and the generated page read that
// door rather than their own query. A loader that failed owes the caller an outage:
// an integer arriving with no error is a collection of that many rows, and no reader
// can tell "nothing here" from "the store would not say". The kernel already states
// the rule for its collaborators — a provider that could not decide answers
// unavailable, never a silent yes — and the total it writes for a module is the same
// answer through the same door.
//
// Two failures are asked for, because they travel differently. A `problem.Problem`
// is a refusal a client can be shown, so it has to arrive as that problem, status and
// detail intact, rather than as a fresh one the door wrote about somebody else's read.
// A plain error is an internal fault, so it has to arrive as that fault: a door that
// turns it into a denial sends a person to ask for a grant they already hold. Both
// arms run the list exactly once, ask the grant exactly once, answer no total of their
// own, and record no denial, because nobody was refused.

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

// TestTheSuppliedCountAnswersAFailedReadWithTheFailureRatherThanANumber registers a
// resource whose list fails — first with a refusal a client could be shown, then with
// a bare internal fault — and asks the total registration supplies for it once as
// each, with the caller's grant held throughout. Each answer owes the caller the
// failure that caused it, no total, the list run once, the grant asked once and no
// denial recorded.
func TestTheSuppliedCountAnswersAFailedReadWithTheFailureRatherThanANumber(t *testing.T) {
	api, _, f := setup(t)

	const readPerm = "billing:catalog"
	const outage = "the plan store is not answering"
	fault := errors.New("the plan page fell over")
	var runs int

	api.RegisterResource(httpx.Resource{
		Module: "billing", Entity: "plan",
		Read: readPerm,
		List: func(_ context.Context, _ crud.Query) ([]map[string]any, int64, error) {
			runs++
			if runs == 1 {
				return nil, 0, problem.New(http.StatusServiceUnavailable, outage)
			}
			return nil, 0, fault
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
	// The grant is held throughout: everything refused below is the read failing and
	// not the caller failing, so any refusal the door answers with is its own invention.
	f.allow = true
	ctx := tenancy.WithPrincipal(tenancy.WithTenant(t.Context(), f.tenant),
		tenancy.Principal{UserID: uuid.New(), Roles: []string{"member"}})

	f.asked.Store(0)
	runs, f.denials = 0, nil
	total, err := door.Count(ctx)
	if err == nil {
		t.Fatalf("the supplied Count answered a list that refused with %d rows and no error: a total with no error is a collection of that many rows, and no reader can tell it from an empty one", total)
	}
	var refused *problem.Problem
	if !errors.As(err, &refused) {
		t.Fatalf("the supplied Count answered its list's refusal with %v, want that refusal: it read a problem a client could be shown and wrote an answer of its own instead", err)
	}
	if refused.Status != http.StatusServiceUnavailable || !strings.Contains(refused.Detail, outage) {
		t.Errorf("the supplied Count restated its list's refusal as %d %q, want the list's own %d %q", refused.Status, refused.Detail, http.StatusServiceUnavailable, outage)
	}
	if total != 0 {
		t.Errorf("the failed Count answered %d beside a failure, want no total: the number it has came out of a read that did not finish", total)
	}
	assertOneReadAskedOnceAndNoDenial(t, f, runs, 1, "the Count whose list refused")

	f.asked.Store(0)
	f.denials = nil
	total, err = door.Count(ctx)
	if !errors.Is(err, fault) {
		t.Errorf("the supplied Count answered its list's fault with %v, want that fault: a door that did not cause the failure has nothing to substitute for it", err)
	}
	var restated *problem.Problem
	if errors.As(err, &restated) && restated.Status < http.StatusInternalServerError {
		t.Errorf("the supplied Count turned its list's internal fault into %d %q: an outage that reads as a denial sends a caller to ask for a grant they already hold", restated.Status, restated.Detail)
	}
	if total != 0 {
		t.Errorf("the failed Count answered %d beside a fault, want no total", total)
	}
	assertOneReadAskedOnceAndNoDenial(t, f, runs, 2, "the Count whose list fell over")
}

// assertOneReadAskedOnceAndNoDenial is what both failures owe in common: the list was
// opened wantRuns times in all, the authorizer was asked once for this call, and
// nothing was recorded as a refusal — the caller held the grant, and the failure
// belongs to the read.
func assertOneReadAskedOnceAndNoDenial(t *testing.T, f *fixture, runs, wantRuns int, whose string) {
	t.Helper()
	if runs != wantRuns {
		t.Errorf("%s read its list %d time(s), want %d in all: a total is one read", whose, runs, wantRuns)
	}
	if got := f.asked.Load(); got != 1 {
		t.Errorf("%s asked the authorizer %d time(s), want once: the caller held the grant, and a failure of the read is not theirs to be asked about twice", whose, got)
	}
	if len(f.denials) != 0 {
		t.Errorf("%s recorded %d denial(s), want none: nobody was refused here, and an outage written down as a denial is the number and the reason both gone", whose, len(f.denials))
	}
}
