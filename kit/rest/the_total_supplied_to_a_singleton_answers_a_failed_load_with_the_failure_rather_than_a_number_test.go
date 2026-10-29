package rest_test

// the_total_supplied_to_a_singleton_answers_a_failed_load_with_the_failure_rather_than_a_number_test.go
// asks the only total a singleton has what it says when the row could not be read.
//
// rest.Singleton hands registration a resource that names no Count of its own, so
// the number an admin card, a catalogue entry or a generated page shows for a
// singleton is the one registration supplies — a total read through the singleton's
// List, which is the singleton's Load. A Load that failed therefore has to travel
// all the way to the card as itself: an integer arriving with no error is a
// collection of that many rows, and no reader can tell "there is none yet" from
// "the store would not say". kit/httpx pins this rule where the list is the
// module's own closure; a singleton reaches the same door through one more wrapper,
// rest.answered, which puts its own translation in front of whatever the Load
// returned, and nothing had yet asked whether the failure is still the failure on
// the far side of that wrapper.
//
// Two failures are asked, because they travel differently. A plain error is an
// internal fault: it has to arrive as that fault, since a door that turns it into a
// denial sends a person to ask for a grant they already hold. A problem the client
// can be shown — a 503 from the store — has to arrive as that problem, status and
// sentence intact, rather than as one the wrapper wrote about it. Each arm reads
// once, is asked for its grant once, answers no total of its own and is refused
// nothing, because nobody was refused.

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// neverLoadedShelf is a singleton whose row this case never lets anyone read: the
// Load below fails before it touches a table, so no table is created for it.
type neverLoadedShelf struct {
	crud.Base
	Note string `json:"note" required:"false" ui:"label:Note"`
}

func (neverLoadedShelf) TableName() string { return "rest_never_loaded_shelves" }

// TestTheSuppliedTotalOfASingletonAnswersAFailedLoadWithTheFailureRatherThanANumber
// mounts a singleton whose Load fails — once with a bare fault, once with a
// client-shown 503 — and asks the total registration supplies for it as each, from
// inside a tenant transaction with the caller's grant held throughout. Each answer
// owes the caller the failure that caused it, no total, one read, one grant ask.
func TestTheSuppliedTotalOfASingletonAnswersAFailedLoadWithTheFailureRatherThanANumber(t *testing.T) {
	_, app := dbtest.Schema(t) // No table: the Load below never reads one.
	ask := &operatorReader{tenant: tenancy.Tenant{ID: uuid.New(), Slug: "acme"}, allow: true}
	api, _ := httpx.New(httpx.Options{
		Cache: cache.Memory("pkit"), PublicHost: host, Tenants: ask, Conn: app,
		Authorize: ask, Installation: host,
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: principal}, true, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	const outage = "the drawing store is not answering"
	fault := errors.New("the drawing shelf fell over")
	var loads int
	shelf := rest.Singleton[*neverLoadedShelf]{
		Module: "drawing", Entity: "shelf", Path: "/shelf", Read: "drawing:read",
		Write: "drawing:write",
		Save: func(_ context.Context, _ db.Tx[db.Tenant], in *neverLoadedShelf) (*neverLoadedShelf, error) {
			return in, nil // never reached: this case only ever asks the total.
		},
		Load: func(context.Context, db.Tx[db.Tenant]) (*neverLoadedShelf, error) {
			loads++
			if loads == 1 {
				return nil, fault
			}
			return nil, problem.New(http.StatusServiceUnavailable, outage)
		},
	}
	shelf.Mount(api.Surfaces(shelf.Module))
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the mounted singleton does not declare itself: %v", err)
	}
	var door httpx.Resource
	for _, r := range api.Resources() {
		if r.Module == "drawing" && r.Entity == "shelf" {
			door = r
		}
	}
	// Registration supplies the total through the Load: a singleton names no Count
	// of its own, so this is the number the card reads. If that supply ever stops,
	// the card has nothing to show and this is the place that says so.
	if door.Count == nil {
		t.Fatal("registration supplied no total for a mounted singleton: the card that reads this collection has no number to show")
	}
	// The grant is held throughout: everything refused below is the read failing,
	// not the caller failing, so any refusal the door answers with is its own.
	ctx := tenancy.WithPrincipal(tenancy.WithTenant(t.Context(), ask.tenant),
		tenancy.Principal{UserID: principal, Roles: []string{"member"}})

	// The door reads through the caller's transaction and owes a 503 without one,
	// so each arm is asked inside db.Run, as the dashboard card asks it.
	var total int64
	var err error
	ask.asked = 0
	if e := db.Run(ctx, app, func(ctx context.Context, _ db.Tx[db.Tenant]) error {
		total, err = door.Count(ctx)
		return nil
	}); e != nil {
		t.Fatalf("the transaction around the total's door failed: %v", e)
	}
	if err == nil {
		t.Fatalf("the singleton's supplied total answered a load that fell over with %d and no error: a total with no error is a collection of that many rows, and no reader can tell it from an empty one", total)
	}
	if !errors.Is(err, fault) {
		t.Errorf("the singleton's supplied total answered its load's fault with %v, want that fault: a door that did not cause the failure has nothing to substitute for it", err)
	}
	if restated := (*problem.Problem)(nil); errors.As(err, &restated) && restated.Status < http.StatusInternalServerError {
		t.Errorf("the singleton's supplied total turned its load's internal fault into %d %q: an outage that reads as a denial sends a caller to ask for a grant they already hold", restated.Status, restated.Detail)
	}
	if total != 0 {
		t.Errorf("the singleton's failed total answered %d beside a failure, want no total: the number it has came out of a read that did not finish", total)
	}
	if loads != 1 {
		t.Errorf("the singleton's total read its row %d times, want once: a total is one read", loads)
	}
	if ask.asked != 1 {
		t.Errorf("the singleton's total asked the authorizer %d times, want once: the caller held the grant, and a failure of the read is not theirs to be asked about twice", ask.asked)
	}

	ask.asked = 0
	if e := db.Run(ctx, app, func(ctx context.Context, _ db.Tx[db.Tenant]) error {
		total, err = door.Count(ctx)
		return nil
	}); e != nil {
		t.Fatalf("the transaction around the total's second answer failed: %v", e)
	}
	var refused *problem.Problem
	if !errors.As(err, &refused) {
		t.Fatalf("the singleton's supplied total answered its load's refusal with %v, want that refusal: it read a problem a client could be shown and wrote an answer of its own instead", err)
	}
	if refused.Status != http.StatusServiceUnavailable || !strings.Contains(refused.Detail, outage) {
		t.Errorf("the singleton's supplied total restated its load's refusal as %d %q, want the load's own %d %q", refused.Status, refused.Detail, http.StatusServiceUnavailable, outage)
	}
	if total != 0 {
		t.Errorf("the singleton's refused total answered %d beside the refusal, want no total", total)
	}
	if loads != 2 {
		t.Errorf("the singleton's total read its row %d times in all, want twice: each total is one read", loads)
	}
	if ask.asked != 1 {
		t.Errorf("the singleton's refused total asked the authorizer %d times, want once", ask.asked)
	}
}
