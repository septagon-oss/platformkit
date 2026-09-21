package rest_test

// The third review's file. Three of the four cases below pin claims the change
// makes about its own new early return that nothing in the tree reaches: the
// commit body says "It sits after `GetForUpdate`, so a PATCH of a missing,
// foreign or soft-deleted row is still 404" — nobody asked the second and third
// of those, on the row shapes where they are not both the same question — and
// "the patch door reads the decoded map … so the door sees what the decoder
// saw", whose media-type case is a create case only. Each case asserts the
// behaviour the change claims, so it passes once the claim is true and fails
// while it is not.

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// Plan is a catalogue row: one list every tenant reads, written by the tenant the
// row names. This is modules/billing's billing_plans — its migration's
// USING (true) WITH CHECK (platformkit_tenant_match(tenant_id)), docs/adr/0008 —
// in the one shape a kit/rest case can mount.
type Plan struct {
	crud.Base
	Name  string `json:"name" validate:"required" ui:"widget:text"`
	Cents int64  `json:"cents,omitempty"`
}

func (Plan) TableName() string { return "rest_review5_plans" }

const planDDL = `
CREATE TABLE rest_review5_plans (
	id uuid PRIMARY KEY,
	tenant_id uuid NOT NULL,
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now(),
	deleted_at timestamptz,
	name text NOT NULL,
	cents bigint NOT NULL DEFAULT 0,
	UNIQUE (tenant_id, name)
);
ALTER TABLE rest_review5_plans ENABLE ROW LEVEL SECURITY;
ALTER TABLE rest_review5_plans FORCE ROW LEVEL SECURITY;
CREATE POLICY rest_review5_plans_catalogue ON rest_review5_plans
	USING (true)
	WITH CHECK (platformkit_tenant_match(tenant_id));`

// mountCatalog mounts the catalogue Spec behind two hosts, so one request can be
// made by a tenant that reads a row and does not own it.
func mountCatalog(t *testing.T) (http.Handler, *sql.DB) {
	t.Helper()
	admin, conn := dbtest.Schema(t)
	if _, err := admin.ExecContext(t.Context(), planDDL); err != nil {
		t.Fatalf("create the catalogue: %v", err)
	}
	tenant2 := tenancy.Tenant{ID: uuid.New(), Slug: "reader", Name: "Reader"}
	api, router := httpx.New(httpx.Options{
		PublicHost: host,
		Tenants:    hosts{map[string]tenancy.Tenant{host: acme, "reader.test": tenant2}},
		Conn:       conn, Authorize: caller{},
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: principal}, true, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	rest.Spec[*Plan]{
		Module: "billing", Entity: "plan", Path: "/plans",
		Read: "billing:read", Write: "billing:catalog",
	}.Mount(api.Surfaces("billing"))
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the mounted routes do not declare themselves: %v", err)
	}
	return router, admin
}

// askAs is call as a named host, which is how one tenant addresses another's row.
func askAs(t *testing.T, r http.Handler, as, method, path, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, "http://"+as+path, strings.NewReader(body))
	req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code, w.Body.String()
}

// TestAPatchThatNamesNoColumnStillRefusesARowAnotherTenantOwns. "A PATCH of a
// missing, foreign or soft-deleted row is still 404" is true of the first and
// third because the new return sits under GetForUpdate. The second is true only
// where the row's own RLS policy hides the row from the request — and on a
// catalogue table, whose policy lets every tenant read one shared list and lets
// only the tenant the row names write it, the row is visible. What refused it
// was the tenant scope kit/crud rechecks inside Update, and a body that names no
// column now stops in front of that recheck: the same door answers 200 with the
// whole row to a tenant that gets 404 for a body naming one column. Nothing is
// written and nothing is published; what moved is that a write door accepted a
// mutation to a row outside the request's tenant and described it back.
func TestAPatchThatNamesNoColumnStillRefusesARowAnotherTenantOwns(t *testing.T) {
	router, admin := mountCatalog(t)

	code, body := call(t, router, http.MethodPost, "/api/v1/billing/plans", `{"name":"Team","cents":4000}`)
	if code != http.StatusCreated {
		t.Fatalf("POST = %d %s", code, body)
	}
	at := "/api/v1/billing/plans/" + id(t, body)

	// The reachability probe, asked of the behaviour that does not move: a body
	// that does name a column is refused for a row the caller can read but does
	// not own. It passes whatever the empty body does, and it is what says this
	// request reached a row outside its own tenant rather than nothing at all.
	if code, out := askAs(t, router, "reader.test", http.MethodPatch, at, `{"name":"rewritten across"}`); code != http.StatusNotFound ||
		strings.Contains(out, "Team") {
		t.Errorf(`reader tenant PATCH {"name":…} = %d %s, want 404 naming no part of the row`, code, out)
	}

	// The same door, the same row, the body that names no column: the same
	// answer, and not the row.
	if code, out := askAs(t, router, "reader.test", http.MethodPatch, at, `{}`); code != http.StatusNotFound ||
		strings.Contains(out, "Team") {
		t.Errorf(`reader tenant PATCH {} = %d %s, want 404 naming no part of the row`, code, out)
	}

	// And the row says nothing happened to it, in either name.
	var name string
	var cents int64
	var owner sql.NullString
	if err := admin.QueryRowContext(t.Context(),
		`SELECT name, cents, tenant_id::text FROM rest_review5_plans WHERE id = $1`, at[strings.LastIndex(at, "/")+1:]).
		Scan(&name, &cents, &owner); err != nil {
		t.Fatalf("read the catalogue row: %v", err)
	}
	if name != "Team" || cents != 4000 {
		t.Errorf("the catalogue row reads %q/%d, want Team/4000", name, cents)
	}
	if n := count(t, admin, "billing.plan.updated"); n != 0 {
		t.Errorf("the two refused patches published %d billing.plan.updated events, want none", n)
	}
	// The row is a real row behind a real policy: the read door does answer it,
	// so the 404 above is about writing, not about a route nobody can reach.
	if code, _ := askAs(t, router, "reader.test", http.MethodGet, at, ""); code != http.StatusOK {
		t.Errorf("reader tenant GET = %d, want 200: the catalogue policy lets every tenant read this row", code)
	}
	if code, _ := askAs(t, router, host, http.MethodGet, at, ""); code != http.StatusOK {
		t.Errorf("owner tenant GET = %d, want 200 for its own row", code)
	}
}

// TestAPatchThatNamesNoColumnRefusesASoftDeletedRow. The third state the commit
// names: a soft-deleted row is not a row, and the empty body neither reads it
// back, moves its stamp nor lifts the hide. The Spec under test has SoftDelete,
// so DELETE keeps the row and hides it.
func TestAPatchThatNamesNoColumnRefusesASoftDeletedRow(t *testing.T) {
	_, router, admin := mounted(t)
	code, body := call(t, router, http.MethodPost, "/api/v1/tasks/task", `{"title":"buried"}`)
	if code != http.StatusCreated {
		t.Fatalf("POST = %d %s", code, body)
	}
	at := "/api/v1/tasks/task/" + id(t, body)
	if code, out := call(t, router, http.MethodDelete, at, ""); code != http.StatusNoContent {
		t.Fatalf("DELETE = %d %s", code, out)
	}
	hidden := func() string {
		var s string
		if err := admin.QueryRowContext(t.Context(),
			`SELECT updated_at::text FROM rest_tasks WHERE id = $1 AND deleted_at IS NOT NULL`,
			at[strings.LastIndex(at, "/")+1:]).Scan(&s); err != nil {
			t.Fatalf("read the buried row (it must still be there and hidden): %v", err)
		}
		return s
	}
	before := hidden()

	for _, sent := range []string{`{}`, `{"title":"resurrected"}`} {
		if code, out := call(t, router, http.MethodPatch, at, sent); code != http.StatusNotFound ||
			strings.Contains(out, "buried") {
			t.Errorf(`PATCH %s on a soft-deleted row = %d %s, want 404 with nothing of the row in it`, sent, code, out)
		}
	}
	if got := hidden(); got != before {
		t.Errorf("a PATCH on a hidden row moved its stamp from %s to %s", before, got)
	}
	if n := count(t, admin, spec.Event(rest.Updated)); n != 0 {
		t.Errorf("the two PATCHes on a hidden row published %d %s events, want none", n, spec.Event(rest.Updated))
	}
	// The control: the same body on a row that is there answers 200, so the 404
	// above is the hidden row and not a route that stopped working.
	if code, out := call(t, router, http.MethodPost, "/api/v1/tasks/task", `{"title":"alive"}`); code != http.StatusCreated {
		t.Fatalf("POST = %d %s", code, out)
	} else if code, out = call(t, router, http.MethodPatch, "/api/v1/tasks/task/"+id(t, out), `{}`); code != http.StatusOK {
		t.Errorf(`PATCH {} on a live row = %d %s, want 200`, code, out)
	}
}

// TestThePatchDoorRefusesAFoldedNameUnderEveryBodyItDecodes. The create door's
// guard reads the request bytes and the change carries a media-type case for it.
// The patch door was given no bytes, and the case for that is "Body carries
// every top-level key the bytes carried" — an argument whose scope is the bodies
// that route actually decodes, which nothing walked. This walks them, and reads
// the row rather than the sentence: no body, under any header, leaves a status a
// command owns on the row, and none of them publishes.
func TestThePatchDoorRefusesAFoldedNameUnderEveryBodyItDecodes(t *testing.T) {
	router, admin := owned(t)
	code, body := call(t, router, http.MethodPost, "/api/v1/tasks/task", `{"title":"untouchable"}`)
	if code != http.StatusCreated {
		t.Fatalf("POST = %d %s", code, body)
	}
	at := "/api/v1/tasks/task/" + id(t, body)
	row := func(column string) string {
		t.Helper()
		var s string
		if err := admin.QueryRowContext(t.Context(),
			`SELECT `+column+` FROM rest_tasks WHERE id = $1`, at[strings.LastIndex(at, "/")+1:]).Scan(&s); err != nil {
			t.Fatalf("read the row's %s: %v", column, err)
		}
		return s
	}
	first := row("status")
	// probes counts the bodies that legitimately wrote the title, each of which
	// publishes the one update the door owes it. Anything above that number is a
	// refusal that announced itself.
	probes := 0

	for _, ct := range []string{
		"application/json",
		"application/json; charset=utf-8",
		"APPLICATION/JSON",
		"application/merge-patch+json",
		"application/json-patch+json",
		"application/vnd.acme.task+json",
		"application/octet-stream",
		"text/plain",
		"application/x-www-form-urlencoded",
		"multipart/form-data; boundary=x",
		"",
	} {
		// Did this body reach the patch door at all? Asked of a body naming a
		// writable field, decided from the row: a 200 that did not land is not a
		// door. It names nothing the refusal would have to print.
		probe := `{"title":"probe ` + ct + `"}`
		reached := false
		if code, _ := callCT(t, router, http.MethodPatch, at, probe, ct); code == http.StatusOK {
			reached = row("title") == "probe "+ct
		}
		if reached {
			probes++
		}
		if !reached {
			t.Logf("Content-Type %q never reaches the patch door", ct)
			continue
		}
		for _, sent := range []string{
			`{"STATUS":"done"}`,
			`{"Status":"done"}`,
			`{"status":"done"}`,
			`{"\u017ftatus":"done"}`,
			`{"title":"mixed","STATUS":"done"}`,
			`{"status":"open","STATUS":"done"}`,
		} {
			if code, out := callCT(t, router, http.MethodPatch, at, sent, ct); code == http.StatusOK {
				t.Errorf(`PATCH %s with Content-Type %q answered 200: %s`, sent, ct, out)
			}
			if got := row("status"); got != first {
				t.Errorf(`PATCH %s with Content-Type %q left status %q, it began %q`, sent, ct, got, first)
				break
			}
		}
	}
	if got := row("status"); got != first {
		t.Errorf("the row ends with status %q, it began %q", got, first)
	}
	var n int
	if err := admin.QueryRowContext(t.Context(),
		`SELECT count(*) FROM rest_tasks WHERE status NOT IN ('', 'open')`).Scan(&n); err != nil {
		t.Fatalf("read the rows: %v", err)
	}
	if n != 0 {
		t.Errorf("%d rows carry a status a command owns", n)
	}
	if events := count(t, admin, spec.Event(rest.Updated)); events != probes {
		t.Errorf("the patch door published %d %s events where %d writes reached it, want the %d it owes",
			events, spec.Event(rest.Updated), probes, probes)
	}
}

// TestThePatchOfNothingAnswersWhatTheReadDoorAnswers. "The row returned as read"
// is deliverable 6's own words, and the change's case compares the empty write
// against the *create's* answer. The read door is the one that reads the row:
// this compares every column of what PATCH {} answers against what GET answers,
// at both doors, and asks it twice so a second empty write cannot drift.
func TestThePatchOfNothingAnswersWhatTheReadDoorAnswers(t *testing.T) {
	api, router, admin := mounted(t)
	r := api.Resources()[0]

	code, body := call(t, router, http.MethodPost, "/api/v1/tasks/task", `{"title":"exact","priority":5}`)
	if code != http.StatusCreated {
		t.Fatalf("POST = %d %s", code, body)
	}
	at := "/api/v1/tasks/task/" + id(t, body)
	read := func(t *testing.T, body string) map[string]any {
		t.Helper()
		var out map[string]any
		if err := json.Unmarshal([]byte(body), &out); err != nil {
			t.Fatalf("read the row in %s: %v", body, err)
		}
		return out
	}

	// What the JSON door answers.
	_, got := call(t, router, http.MethodGet, at, "")
	wanted := read(t, got)
	code, body = call(t, router, http.MethodPatch, at, `{}`)
	if code != http.StatusOK {
		t.Fatalf("PATCH {} = %d %s", code, body)
	}
	gotJSON := read(t, body)
	for column, want := range wanted {
		if gotJSON[column] != want {
			t.Errorf(`PATCH {} answered %s as %v, GET answers %v`, column, gotJSON[column], want)
		}
	}
	for column := range gotJSON {
		if _, named := wanted[column]; !named {
			t.Errorf(`PATCH {} answered a column %s the read door does not name`, column)
		}
	}

	// What the door beneath HTTP answers, from the same row.
	var seen map[string]any
	var failures []string
	// The probe is a route of the tasks module on the workspace surface, so its
	// address is composed from that rather than written here.
	probe := api.Surfaces(spec.Module).App
	httpx.Register(probe, huma.Operation{
		OperationID: "patch-nothing", Method: http.MethodPost, Path: "/probe/patch-nothing", Hidden: true,
		DefaultStatus: http.StatusNoContent,
	}, httpx.SignedIn(), func(ctx context.Context, _ *struct{}) (*struct{}, error) {
		out, err := r.Update(ctx, uuid.MustParse(at[strings.LastIndex(at, "/")+1:]), map[string]any{})
		if err != nil {
			failures = append(failures, "the page's Update: "+err.Error())
			return nil, nil
		}
		seen = out
		return nil, nil
	})
	if code, out := call(t, router, http.MethodPost, probe.Path("/probe/patch-nothing"), ""); code != http.StatusNoContent {
		failures = append(failures, "the probe request = "+http.StatusText(code)+" "+out)
	}
	for _, f := range failures {
		t.Error(f)
	}
	for column, want := range wanted {
		if seen[column] != want {
			t.Errorf(`the page's PATCH of nothing answered %s as %v, GET answers %v`, column, seen[column], want)
		}
	}
	if n := count(t, admin, spec.Event(rest.Updated)); n != 0 {
		t.Errorf("the two empty writes published %d %s events, want none", n, spec.Event(rest.Updated))
	}
	// The control: a body that names a column changes what both doors answer,
	// so the equality above is a row and not an empty map.
	if code, body = call(t, router, http.MethodPatch, at, `{"title":"exact again"}`); code != http.StatusOK {
		t.Fatalf(`PATCH {"title":…} = %d %s`, code, body)
	} else if read(t, body)["title"] != "exact again" {
		t.Errorf("a PATCH that named a column answered %s", body)
	}
}
