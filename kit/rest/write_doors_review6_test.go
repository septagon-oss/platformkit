package rest_test

// The fourth review's file. Round 5 moved two things — the empty PATCH's tenant
// recheck and the delete door's — and both new returns land the same answer on
// row shapes that used to answer differently. What nothing in the tree pins
// after those moves is:
//
//  1. that the two delete statements the round-5 case's own comment calls the
//     whole difference are still two different statements. After the recheck
//     `deleteDoorRefused` asserts the identical thing for SoftDelete true and
//     false — 404, one live row, no event — so nothing any more asks what each
//     statement does when the tenant *is* allowed: a Spec whose
//     `SoftDelete: false` quietly became a hide keeps every existing case green.
//  2. the two halves of the acceptance that sit on the far side of each new
//     case: the caller's *own* row on the shared catalogue answers 200 and says
//     nothing (round 3's file asserts the silence on rest_tasks, whose policy
//     hides foreign rows and which is therefore not the row shape the finding
//     was about), and the door *beneath* HTTP refuses a foreign row the way the
//     route does (round 5's file asks tenancy at the route and the in-process
//     doors only inside the tenant that owns the row).
//
// Every case asserts behaviour the change claims, so each passes once the claim
// is true and fails while it is not, and each reaches its assertion through a
// status, an error kind, the row's own key and the outbox — never through what a
// defect prints.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// sharedCatalogue is mountCatalog's mount given the soft-delete flag and the
// API, so one case can ask both statements a delete becomes and reach the two
// closures a page calls as well as the five routes.
func sharedCatalogue(t *testing.T, soft bool) (*httpx.API, http.Handler, *sql.DB) {
	t.Helper()
	admin, conn := dbtest.Schema(t)
	if _, err := admin.ExecContext(t.Context(), planDDL); err != nil {
		t.Fatalf("create the catalogue: %v", err)
	}
	reader := tenancy.Tenant{ID: uuid.New(), Slug: "reader", Name: "Reader"}
	api, router := httpx.New(httpx.Options{
		PublicHost: host,
		Tenants:    hosts{map[string]tenancy.Tenant{host: acme, "reader.test": reader}},
		Conn:       conn, Authorize: caller{},
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: principal}, true, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	rest.Spec[*Plan]{
		Module: "billing", Entity: "plan", Path: "/api/v1/billing/plans",
		Read: "billing:read", Write: "billing:catalog", SoftDelete: soft,
	}.Mount(api)
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the mounted routes do not declare themselves: %v", err)
	}
	return api, router, admin
}

// planRow is the catalogue row as the table holds it: how many physical copies,
// how many live, and what the one that is there is called.
type planRow struct {
	physical, live int
	name           string
}

func readPlan(t *testing.T, admin *sql.DB, key string) planRow {
	t.Helper()
	var row planRow
	if err := admin.QueryRowContext(t.Context(),
		`SELECT count(*) FROM rest_review5_plans WHERE id = $1`, key).Scan(&row.physical); err != nil {
		t.Fatalf("count the row's copies: %v", err)
	}
	if err := admin.QueryRowContext(t.Context(),
		`SELECT count(*) FROM rest_review5_plans WHERE id = $1 AND deleted_at IS NULL`, key).Scan(&row.live); err != nil {
		t.Fatalf("count the live copies: %v", err)
	}
	// max()/coalesce() rather than a bare Scan: a physically removed row has to
	// answer "" here, because whether it was removed at all is the question.
	if err := admin.QueryRowContext(t.Context(),
		`SELECT coalesce(max(name), '') FROM rest_review5_plans WHERE id = $1`, key).Scan(&row.name); err != nil {
		t.Fatalf("read the row's name: %v", err)
	}
	return row
}

// hiddenPlan counts the copies the table still holds with the hide set.
func hiddenPlan(t *testing.T, admin *sql.DB, key string) int {
	t.Helper()
	var n int
	if err := admin.QueryRowContext(t.Context(),
		`SELECT count(*) FROM rest_review5_plans WHERE id = $1 AND deleted_at IS NOT NULL`, key).Scan(&n); err != nil {
		t.Fatalf("count the hidden copies: %v", err)
	}
	return n
}

// stampOf is the instant the row's own column holds, the only answer to "did
// that write reach the database".
func stampOf(t *testing.T, admin *sql.DB, key string) string {
	t.Helper()
	var at string
	if err := admin.QueryRowContext(t.Context(),
		`SELECT updated_at::text FROM rest_review5_plans WHERE id = $1`, key).Scan(&at); err != nil {
		t.Fatalf("read the row's stamp: %v", err)
	}
	return at
}

// inPage registers a hidden route whose handler runs body inside a request, so a
// case can drive the two closures a page calls under whichever host the request
// carries. What the closures did is collected in fail rather than returned: a
// returned error would be answered through the kernel's own mapping, and these
// cases are about the input to that mapping.
func inPage(t *testing.T, api *httpx.API, path, verb string, fail *[]string,
	body func(context.Context, httpx.Resource, *[]string)) {
	t.Helper()
	httpx.Register(api, huma.Operation{
		OperationID: "review6-" + verb, Method: http.MethodPost, Path: path,
		Hidden: true, DefaultStatus: http.StatusNoContent,
	}, httpx.SignedIn(), func(ctx context.Context, _ *struct{}) (*struct{}, error) {
		body(ctx, api.Resources()[0], fail)
		return nil, nil
	})
}

// TestTheDeleteDoorStillTellsARemovedRowFromAHiddenOne. The refused delete is one
// answer for both flags — that is round 5's fix and the point of its case. The
// delete that is not refused is two different things, and the Spec's flag picks
// which: a Spec that says SoftDelete keeps the row and hides it; a Spec that does
// not says the row leaves the table.
func TestTheDeleteDoorStillTellsARemovedRowFromAHiddenOne(t *testing.T) {
	for _, soft := range []bool{false, true} {
		t.Run("soft="+strconv.FormatBool(soft), func(t *testing.T) {
			_, router, admin := sharedCatalogue(t, soft)
			code, body := call(t, router, http.MethodPost, "/api/v1/billing/plans", `{"name":"Team","cents":4000}`)
			if code != http.StatusCreated {
				t.Fatalf("POST = %d %s", code, body)
			}
			key := id(t, body)
			at := "/api/v1/billing/plans/" + key
			before := readPlan(t, admin, key)
			if before.physical != 1 || before.live != 1 || before.name != "Team" {
				t.Fatalf("the create left %+v, want one live row called Team", before)
			}

			// The tenant that may read the row and not remove it: refused, with
			// nothing of the row said and nothing about it moved.
			if code, out := askAs(t, router, "reader.test", http.MethodDelete, at, ""); code != http.StatusNotFound ||
				strings.Contains(out, "Team") {
				t.Fatalf("reader tenant DELETE = %d %s, want 404 naming no part of the row", code, out)
			}
			if got := readPlan(t, admin, key); got != before {
				t.Errorf("the refused delete left %+v where the row was %+v", got, before)
			}
			if n := count(t, admin, "billing.plan.deleted"); n != 0 {
				t.Errorf("the refused delete published %d billing.plan.deleted events, want none", n)
			}

			// The owner — the control that makes the refusal above about a row
			// rather than about a route that stopped working, and the one answer
			// the two flags must not share.
			if code, out := call(t, router, http.MethodDelete, at, ""); code != http.StatusNoContent {
				t.Fatalf("owner DELETE = %d %s, want 204", code, out)
			}
			after := readPlan(t, admin, key)
			if after.live != 0 {
				t.Errorf("the owner's delete left %d live copies, want none", after.live)
			}
			wantPhysical, wantHidden := 0, 0
			if soft {
				wantPhysical, wantHidden = 1, 1
			}
			if after.physical != wantPhysical {
				t.Errorf("SoftDelete=%v: the owner's delete left %d copies in the table, want %d",
					soft, after.physical, wantPhysical)
			}
			if got := hiddenPlan(t, admin, key); got != wantHidden {
				t.Errorf("SoftDelete=%v: the owner's delete left %d copies hidden but kept, want %d",
					soft, got, wantHidden)
			}
			if n := count(t, admin, "billing.plan.deleted"); n != 1 {
				t.Errorf("the owner's delete published %d billing.plan.deleted events, want the one it owes", n)
			}
		})
	}
}

// TestTheOwnersEmptyPatchOnASharedRowAnswersItsOwnRowAndSaysNothing. The
// acceptance's second half: whose row it is gets settled first, and once the row
// is the caller's an empty PATCH says nothing. This asks it on the catalogue
// mount — shared table, permissive read policy, soft-delete flag, no immutable
// field — because every silence case in the package runs against rest_tasks.
func TestTheOwnersEmptyPatchOnASharedRowAnswersItsOwnRowAndSaysNothing(t *testing.T) {
	_, router, admin := sharedCatalogue(t, true)
	code, body := call(t, router, http.MethodPost, "/api/v1/billing/plans", `{"name":"Team","cents":4000}`)
	if code != http.StatusCreated {
		t.Fatalf("POST = %d %s", code, body)
	}
	key := id(t, body)
	at := "/api/v1/billing/plans/" + key
	stamped, stored := answerStamp(t, body), stampOf(t, admin, key)

	if code, body := askAs(t, router, host, http.MethodPatch, at, `{}`); code != http.StatusOK {
		t.Fatalf("owner PATCH {} = %d %s, want 200 with its own row", code, body)
	} else if answerStamp(t, body) != stamped {
		t.Errorf("owner PATCH {} answered a row stamped %s, the create stamped it %s", answerStamp(t, body), stamped)
	} else if !strings.Contains(body, `"name":"Team"`) || !strings.Contains(body, `"cents":4000`) {
		t.Errorf("owner PATCH {} answered %s, want the row as it stands", body)
	}
	if got := stampOf(t, admin, key); got != stored {
		t.Errorf("owner PATCH {} wrote the row: updated_at went %s to %s", stored, got)
	}
	if n := count(t, admin, "billing.plan.updated"); n != 0 {
		t.Errorf("the owner's empty PATCH published %d billing.plan.updated events, want none", n)
	}
	// The other tenant still reads the row it was always allowed to read, so the
	// silence above is not the row having gone somewhere.
	if code, out := askAs(t, router, "reader.test", http.MethodGet, at, ""); code != http.StatusOK ||
		!strings.Contains(out, "Team") {
		t.Errorf("reader tenant GET = %d %s, want 200 with the shared row", code, out)
	}

	// The control, so the silence cannot be a route that stopped working: the
	// same door, one column named, writes, stamps and publishes exactly once.
	if code, body := askAs(t, router, host, http.MethodPatch, at, `{"cents":5000}`); code != http.StatusOK {
		t.Fatalf(`owner PATCH {"cents":5000} = %d %s`, code, body)
	}
	if got := stampOf(t, admin, key); got == stored {
		t.Errorf("a PATCH that named a column left updated_at at %s", got)
	}
	if n := count(t, admin, "billing.plan.updated"); n != 1 {
		t.Errorf("a PATCH that named a column published %d billing.plan.updated events, want the one", n)
	}
}

func answerStamp(t *testing.T, body string) string {
	t.Helper()
	var out struct {
		UpdatedAt string `json:"updatedAt"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil || out.UpdatedAt == "" {
		t.Fatalf("no updatedAt in %s: %v", body, err)
	}
	return out.UpdatedAt
}

// TestThePageDoorRefusesARowAnotherTenantOwnsTheWayTheRouteDoes. The recheck
// lives in updateRow and deleteRow, which the five routes and the two closures
// share, so the page's answer about a foreign catalogue row is claimed to be the
// route's: ErrNotFound, nothing of the row in it, the row standing, nothing
// published. Round 5's file asks tenancy through HTTP and asks the closures only
// inside the tenant that owns the row, so this is the case that says the fix is
// in the shared half and not in the HTTP half.
func TestThePageDoorRefusesARowAnotherTenantOwnsTheWayTheRouteDoes(t *testing.T) {
	api, router, admin := sharedCatalogue(t, false)

	// A row the acme host owns, which the reader host may read and may not write.
	code, body := call(t, router, http.MethodPost, "/api/v1/billing/plans", `{"name":"Team","cents":4000}`)
	if code != http.StatusCreated {
		t.Fatalf("POST = %d %s", code, body)
	}
	foreign := uuid.MustParse(id(t, body))
	// And a row the reader host does own: the control, the same two closures, an
	// answer that must not be ErrNotFound.
	code, body = askAs(t, router, "reader.test", http.MethodPost, "/api/v1/billing/plans", `{"name":"Mine","cents":1000}`)
	if code != http.StatusCreated {
		t.Fatalf("reader POST = %d %s", code, body)
	}
	mine := uuid.MustParse(id(t, body))

	var refused, control []string
	inPage(t, api, "/review6/foreign", "foreign", &refused, func(ctx context.Context, r httpx.Resource, fail *[]string) {
		if row, err := r.Update(ctx, foreign, map[string]any{}); err == nil {
			*fail = append(*fail, "the page's Update of nothing returned name "+fmt.Sprintf("%v", row["name"])+" and said nothing was wrong")
		} else if code := faultStatus(err); code != http.StatusNotFound {
			*fail = append(*fail, fmt.Sprintf("the page's Update of nothing: %d %v, want the 404 the route answers", code, err))
		} else if strings.Contains(err.Error(), "Team") {
			*fail = append(*fail, "the page's Update of nothing named the row: "+err.Error())
		}
		if err := r.Delete(ctx, foreign); err == nil {
			*fail = append(*fail, "the page's Delete removed a row this tenant does not own and said nothing was wrong")
		} else if code := faultStatus(err); code != http.StatusNotFound {
			*fail = append(*fail, fmt.Sprintf("the page's Delete: %d %v, want the 404 the route answers", code, err))
		} else if strings.Contains(err.Error(), "Team") {
			*fail = append(*fail, "the page's Delete named the row: "+err.Error())
		}
	})
	inPage(t, api, "/review6/own", "own", &control, func(ctx context.Context, r httpx.Resource, fail *[]string) {
		if row, err := r.Update(ctx, mine, map[string]any{}); err != nil {
			*fail = append(*fail, "the page's Update of nothing on the tenant's own row: "+err.Error())
		} else if fmt.Sprintf("%v", row["name"]) != "Mine" {
			*fail = append(*fail, "the page's Update of nothing answered name "+fmt.Sprintf("%v", row["name"])+", want Mine")
		}
		if err := r.Delete(ctx, mine); err != nil {
			*fail = append(*fail, "the page's Delete of the tenant's own row: "+err.Error())
		}
	})

	// The foreign row first, and the counts asked before the control runs, so a
	// published event cannot be attributed to the wrong half of the case.
	if code, out := askAs(t, router, "reader.test", http.MethodPost, "/review6/foreign", ""); code != http.StatusNoContent {
		t.Fatalf("the foreign-row probe request = %d %s, want 204 so the closures were reached", code, out)
	}
	for _, f := range refused {
		t.Error(f)
	}
	if got := readPlan(t, admin, foreign.String()); got.live != 1 || got.name != "Team" {
		t.Errorf("the page's refused writes left %+v, want the row live and called Team", got)
	}
	if n := count(t, admin, "billing.plan.updated"); n != 0 {
		t.Errorf("the page's refused writes published %d billing.plan.updated events, want none", n)
	}
	if n := count(t, admin, "billing.plan.deleted"); n != 0 {
		t.Errorf("the page's refused delete published %d billing.plan.deleted events, want none", n)
	}

	if code, out := askAs(t, router, "reader.test", http.MethodPost, "/review6/own", ""); code != http.StatusNoContent {
		t.Fatalf("the own-row probe request = %d %s, want 204 so the control ran", code, out)
	}
	for _, f := range control {
		t.Error(f)
	}
	// The control row did go, so the refusals above are about ownership and not
	// about closures that refuse everything — and it went with the one event a
	// real delete owes, so the count above was counting something.
	if got := readPlan(t, admin, mine.String()); got.live != 0 {
		t.Errorf("the control: the page's own delete left %d live copies of the reader's own row", got.live)
	}
	if n := count(t, admin, "billing.plan.deleted"); n != 1 {
		t.Errorf("the two deletes published %d billing.plan.deleted events, want the one the control's own delete owes", n)
	}
	// The control's empty Update is the caller's own row, and it said nothing.
	if n := count(t, admin, "billing.plan.updated"); n != 0 {
		t.Errorf("the page's empty writes published %d billing.plan.updated events, want none", n)
	}
}

// faultStatus is the status the door beneath HTTP answered with, which is the
// same decision the route's own mapping makes: rest.Fault turns crud.ErrNotFound
// into a 404 problem rather than handing the sentinel back.
func faultStatus(err error) int {
	if p, ok := errors.AsType[*problem.Problem](err); ok {
		return p.Status
	}
	return 0
}
