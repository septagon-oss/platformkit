package rest_test

// operations_test.go is T-0185: a resource states which of the five routes it
// offers, and may state its guard as a declaration instead of a permission.
//
// Every case here mounts a real Spec through the real API and answers real
// HTTP, because the claim is about the route table, the generated screens and
// the catalogue at once — and the two cases that ask which rows a caller sees
// ask a real Postgres, which dbtest refuses to skip.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// The caller every signed-in case here is made by is the package's own
// `member` with nothing in it: an Authorizer that answers yes to exactly the
// permissions it holds, which for this map is none. That is the caller
// httpx.SignedIn() admits and every httpx.Permission refuses.

// everyPlan answers "yes, it is in the plan" to the one case that mounts a
// declaration needing a feature, which is what Options.Entitle exists for.
type everyPlan struct{}

func (everyPlan) Includes(context.Context, tenancy.Tenant, string) (bool, error) { return true, nil }

// callHost is call at a host of the case's choosing, which is how one request
// asks about one tenant and the next about another.
func callHost(t *testing.T, r http.Handler, method, at, path, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, "http://"+at+path, strings.NewReader(body))
	req.Host = strings.TrimPrefix(at, "http://")
	req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code, w.Body.String()
}

// callAnonymous is call with nothing to recognise: no session cookie, so the
// identity hook is never asked and the request stays anonymous. Compare
// rest_test.go's call, which brings one.
func callAnonymous(t *testing.T, r http.Handler, method, path string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, "http://"+host+path, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code, w.Body.String()
}

// mountPlain is mount for a Spec whose guard needs what the shared harness does
// not compose — today, a plan feature, which Options.Entitle answers and the
// harness has no reason to carry. Everything else about the chain is the same.
func mountPlain(t *testing.T, s rest.Spec[*Task], authorize httpx.Authorizer) (*httpx.API, http.Handler, *sql.DB) {
	t.Helper()
	admin, app := dbtest.Schema(t)
	if _, err := admin.ExecContext(t.Context(), ddl); err != nil {
		t.Fatalf("create tasks: %v", err)
	}
	loader := httpx.TenantLoader(caller{})
	if l, ok := authorize.(httpx.TenantLoader); ok {
		loader = l
	}
	api, router := httpx.New(httpx.Options{
		PublicHost: host, Tenants: loader, Conn: app, Authorize: authorize, Entitle: everyPlan{},
		Installation: host,
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: principal}, true, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	s.Mount(api.Surfaces(s.Module))
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the mounted routes do not declare themselves: %v", err)
	}
	return api, router, admin
}

// refusesToMount mounts a Spec that must not mount, and reads the sentence the
// mount site panicked with. The panic is how every wiring rule in check() is
// refused — where the mistake was written, and before any route exists.
// probeOperation is a probe route: a handler that runs inside one request, in
// which a case can call a resource closure or answer about the caller.
func probeOperation(id, path string) huma.Operation {
	return huma.Operation{OperationID: id, Method: http.MethodPost, Path: path, DefaultStatus: http.StatusNoContent}
}

func refusesToMount(t *testing.T, s rest.Spec[*Task], want string) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Errorf("the Spec mounted; want a refusal naming %q", want)
			return
		}
		if msg := fmt.Sprint(r); !strings.Contains(msg, want) {
			t.Errorf("the mount site says %q, want it to name %q", msg, want)
		}
	}()
	_, _, _ = mount(t, s)
}

// operationIDs are the verbs this API actually mounted, as the OpenAPI document
// and the boot gate see them.
func operationIDs(api *httpx.API) []string {
	var out []string
	for _, op := range api.Recorded() {
		out = append(out, op.OperationID)
	}
	return out
}

func recorded(t *testing.T, api *httpx.API, id string) *huma.Operation {
	t.Helper()
	for _, op := range api.Recorded() {
		if op.OperationID == id {
			return op
		}
	}
	return nil
}

// TestTheDefaultSpecStillMountsAllFiveUnderBothPermissions is the zero value: a
// Spec that names no operations and no guard declaration mounts what it mounted
// before those fields existed, guarded by the same two permission strings. The
// brief's "existing specs are unchanged" is this case, and the three cases
// beside it in the package's own suite (TestTheFiveRoutesAnswer,
// TestEveryRouteCarriesItsDeclaration, TestMountRegistersTheEntityBesideItsRoutes)
// must stay green without one edit of their own.
func TestTheDefaultSpecStillMountsAllFiveUnderBothPermissions(t *testing.T) {
	api, _, _ := mounted(t)
	want := []string{
		"tasks-task-list", "tasks-task-create", "tasks-task-read", "tasks-task-update", "tasks-task-delete",
	}
	for _, id := range want {
		op := recorded(t, api, id)
		if op == nil {
			t.Errorf("%s is not mounted; the zero Operations no longer means all five", id)
			continue
		}
		kind, _ := op.Extensions[httpx.AuthExtension].(httpx.Auth)
		permission := "task:read"
		if strings.HasSuffix(id, "create") || strings.HasSuffix(id, "update") || strings.HasSuffix(id, "delete") {
			permission = "task:write"
		}
		if kind.String() != "permission "+permission {
			t.Errorf("%s declares %s, want permission %s", id, kind, permission)
		}
	}
	if got := api.Resources()[0].OperationWords(); got != nil {
		t.Errorf("a resource offering all five says %v, want the empty answer that means all five", got)
	}
}

// TestASpecOfferingListAndReadMountsNoWriteRoute is the brief's first sentence:
// the write routes are not mounted, so the address answers the 404 of a place
// nothing is served — not a 405, and not a refusal naming a permission nobody
// asked for. The document says the same thing, because it is written from the
// same five calls.
func TestASpecOfferingListAndReadMountsNoWriteRoute(t *testing.T) {
	only := spec
	only.Operations = []httpx.CRUD{rest.List, rest.Read}
	api, router, _ := mount(t, only)

	if code, body := call(t, router, http.MethodGet, "/api/v1/tasks/task", ""); code != http.StatusOK {
		t.Fatalf("GET the collection = %d %s, want 200: list is offered", code, body)
	}
	row := uuid.New()
	// A withheld verb answers according to whether the address exists. The
	// collection and the item are mounted — for the two reads — so the router
	// says this address does not accept that method; the sentence is the router's
	// own, not a refusal naming a permission nobody asked for. TestTheItemAddress
	//OfAResourceThatOffersNoReadIsNotFound is the other half: an address nothing
	// mounted at all is the 404 of a place nothing is served.
	for _, asked := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/tasks/task"},
		{http.MethodPatch, "/api/v1/tasks/task/" + row.String()},
		{http.MethodDelete, "/api/v1/tasks/task/" + row.String()},
	} {
		code, body := call(t, router, asked.method, asked.path, `{"title":"written where nothing listens"}`)
		if code != http.StatusMethodNotAllowed {
			t.Errorf("%s %s = %d %s, want %d: the address is mounted for a read and this verb is not",
				asked.method, asked.path, code, body, http.StatusMethodNotAllowed)
		}
	}
	for _, id := range operationIDs(api) {
		if slices.Contains([]string{"tasks-task-create", "tasks-task-update", "tasks-task-delete"}, id) {
			t.Errorf("%s is mounted, which the Spec did not offer", id)
		}
	}
	// The events follow the routes: the boot gate reads the declared events off
	// the operations that exist, so a create-less resource declares no create
	// event to it and nothing else about the two remaining routes moved.
	for _, id := range []string{"tasks-task-list", "tasks-task-read"} {
		if recorded(t, api, id) == nil {
			t.Errorf("%s is gone, and only its three siblings were withheld", id)
		}
	}
	if events := api.Events(); len(events) != 0 {
		t.Errorf("a resource with no mounted write publishes %v to the event gate, want nothing", events)
	}
}

// TestTheItemAddressOfAResourceThatOffersNoReadIsNotFound is the other half of
// a withheld verb: when no route is mounted at the address either, the answer is
// the 404 of a place nothing is served, and not a 405 about a method an address
// nobody claimed.
func TestTheItemAddressOfAResourceThatOffersNoReadIsNotFound(t *testing.T) {
	collectionOnly := spec
	collectionOnly.Operations = []httpx.CRUD{rest.List}
	_, router, _ := mount(t, collectionOnly)
	if code, body := call(t, router, http.MethodGet, "/api/v1/tasks/task/"+uuid.New().String(), ""); code != http.StatusNotFound {
		t.Errorf("GET the item of a list-only resource = %d %s, want 404", code, body)
	}
}

// TestTheClosuresOfAnUnofferedVerbRefuseAndNeverPanic is the half a generated
// page or a hand-written one meets: the screen calls all five closures whatever
// the resource mounted, and rest.Singleton already makes that a sentence rather
// than a nil dereference.
func TestTheClosuresOfAnUnofferedVerbRefuseAndNeverPanic(t *testing.T) {
	only := spec
	only.Operations = []httpx.CRUD{rest.List, rest.Read}
	api, router, admin := mount(t, only)
	if _, err := admin.ExecContext(t.Context(),
		"INSERT INTO rest_tasks (id, tenant_id, title) VALUES ($1, $2, $3)",
		uuid.New(), acme.ID, "a row of acmes"); err != nil {
		t.Fatal(err)
	}
	res := api.Resources()[0]

	httpx.Register(api.Surfaces("tasks").App, probeOperation("probe", "/probe"),
		httpx.SignedIn(), func(ctx context.Context, _ *struct{}) (*struct{}, error) {
			if _, err := res.Create(ctx, map[string]any{"title": "through the page"}); !refused(err, http.StatusConflict) {
				t.Errorf("Create = %v, want a 409 naming the door", err)
			}
			if _, err := res.Update(ctx, uuid.New(), map[string]any{"title": "x"}); !refused(err, http.StatusConflict) {
				t.Errorf("Update = %v, want a 409 naming the door", err)
			}
			if err := res.Delete(ctx, uuid.New()); !refused(err, http.StatusConflict) {
				t.Errorf("Delete = %v, want a 409 naming the door", err)
			}
			// The verb it does offer still works, through the same closure set.
			rows, total, err := res.List(ctx, crud.Query{Limit: 10})
			if err != nil || total != 1 || len(rows) != 1 {
				t.Errorf("List = %v, %d, %v, want the tenant's one row", rows, total, err)
			}
			return nil, nil
		})
	if code, body := call(t, router, http.MethodPost, api.Surfaces("tasks").App.Path("/probe"), ""); code != http.StatusNoContent {
		t.Fatalf("the probe = %d %s", code, body)
	}
}

// refused reports whether err is the problem document of one status.
func refused(err error, status int) bool {
	p, ok := errors.AsType[*problem.Problem](err)
	return ok && p.Status == status
}

// TestASignedInReadRouteAdmitsAMemberHoldingNothingAndRefusesAnAnonymousCaller
// is the brief's second sentence at the route: httpx.SignedIn() is the whole
// guard, so a member of the resolved tenant who holds no grant at all is served,
// and the same request without a caller is refused in the shape that says why.
// It also asks the catalogue's question — Readable — because a resource Describe
// drops for a caller who can reach every row of it would be a worse lie than the
// one this field replaced.
func TestASignedInReadRouteAdmitsAMemberHoldingNothingAndRefusesAnAnonymousCaller(t *testing.T) {
	shared := spec
	shared.Read = ""
	shared.ReadAuth = httpx.SignedIn()
	// Write stays a permission: a signed-in write whose row the caller names is
	// refused at the mount site, and TestASignedInWriteGuardRefusesTheGenericWrites
	// is that case. Withholding update and delete leaves a create, which is the
	// one generic write that names no row.
	shared.Operations = []httpx.CRUD{rest.List, rest.Read, rest.Create}
	shared.Write = "task:write"

	api, router, _ := mountAs(t, shared, member{})
	if code, body := call(t, router, http.MethodGet, "/api/v1/tasks/task", ""); code != http.StatusOK {
		t.Fatalf("a member holding nothing read the collection at %d %s, want 200", code, body)
	}
	if code, body := callAnonymous(t, router, http.MethodGet, "/api/v1/tasks/task"); code != http.StatusForbidden ||
		!strings.Contains(body, httpx.CodeAnonymous) {
		t.Errorf("an anonymous caller read the collection at %d %s, want %s", code, body, httpx.CodeAnonymous)
	}
	// Through the resource, which is what the catalogue and a page read.
	res := api.Resources()[0]
	var readable, writable bool
	httpx.Register(api.Surfaces("tasks").App, probeOperation("probe", "/probe"),
		httpx.SignedIn(), func(ctx context.Context, _ *struct{}) (*struct{}, error) {
			readable, writable = res.Readable(ctx), res.Writable(ctx)
			return nil, nil
		})
	if code, body := call(t, router, http.MethodPost, api.Surfaces("tasks").App.Path("/probe"), ""); code != http.StatusNoContent {
		t.Fatalf("the probe = %d %s", code, body)
	}
	if !readable {
		t.Error("Readable is false for a member the guard admits, so Describe would drop this resource from their catalog")
	}
	if writable {
		t.Error("Writable is true for a member who holds no task:write, so a screen would draw a door the API refuses")
	}
}

// TestAPermissionSpecStillRefusesTheSameMember keeps the default honest: the
// caller above, the same request, against the unchanged Spec, is refused. The
// new fields change nothing for a Spec that does not name them.
func TestAPermissionSpecStillRefusesTheSameMember(t *testing.T) {
	_, router, _ := mountAs(t, spec, member{})
	code, body := call(t, router, http.MethodGet, "/api/v1/tasks/task", "")
	if code != http.StatusForbidden || !strings.Contains(body, httpx.CodeDenied) {
		t.Fatalf("a member holding no grant read a permission-guarded list at %d %s, want %s", code, body, httpx.CodeDenied)
	}
}

// TestASignedInListSeesExactlyTheRowsItsTenantMayRead is tenant-first, and the
// sentence in the middle of it matters: what limits this list is the table's own
// row-level security policy under the request's tenant transaction — not object
// scope, which the generic routes do not offer and rest.Command is the door for.
// A signed-in guard changes who may ask, and nothing about whose rows the answer
// can contain.
func TestASignedInListSeesExactlyTheRowsItsTenantMayRead(t *testing.T) {
	acmeHost, globexHost := "acme.test", "globex.test"
	globex := tenancy.Tenant{ID: uuid.New(), Slug: "globex", Name: "Globex"}
	shared := spec
	shared.Operations = []httpx.CRUD{rest.List, rest.Read}
	shared.Read = ""
	shared.ReadAuth = httpx.SignedIn()
	admin, app := dbtest.Schema(t)
	if _, err := admin.ExecContext(t.Context(), ddl); err != nil {
		t.Fatal(err)
	}
	acmeRow, globexRow := uuid.New(), uuid.New()
	for _, row := range []struct {
		id     uuid.UUID
		tenant uuid.UUID
		title  string
	}{{acmeRow, acme.ID, "acmes own"}, {globexRow, globex.ID, "globexs own"}} {
		if _, err := admin.ExecContext(t.Context(),
			"INSERT INTO rest_tasks (id, tenant_id, title) VALUES ($1, $2, $3)", row.id, row.tenant, row.title); err != nil {
			t.Fatal(err)
		}
	}
	api, router := httpx.New(httpx.Options{
		PublicHost: acmeHost, Installation: acmeHost,
		Tenants:   hosts{byHost: map[string]tenancy.Tenant{acmeHost: acme, globexHost: globex}},
		Conn:      app,
		Authorize: member{},
		Entitle:   everyPlan{},
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: principal}, true, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	shared.Mount(api.Surfaces(shared.Module))
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatal(err)
	}
	listed := func(at string) string {
		code, body := callHost(t, router, http.MethodGet, at, "/api/v1/tasks/task", "")
		if code != http.StatusOK {
			t.Fatalf("%s listing its tasks = %d %s", at, code, body)
		}
		return body
	}
	if acmeList := listed(acmeHost); !strings.Contains(acmeList, "acmes own") || strings.Contains(acmeList, "globexs own") {
		t.Errorf("acme's list is %s, want its own row and no other tenant's", acmeList)
	}
	if globexList := listed(globexHost); !strings.Contains(globexList, "globexs own") || strings.Contains(globexList, "acmes own") {
		t.Errorf("globex's list is %s, want its own row and no other tenant's", globexList)
	}
	// The item door says the same thing, in the shape that does not disclose
	// that the row exists at all: 404, from the policy's USING clause.
	for _, at := range []string{acmeHost, globexHost} {
		foreign := globexRow
		if at == globexHost {
			foreign = acmeRow
		}
		if code, body := callHost(t, router, http.MethodGet, at, "/api/v1/tasks/task/"+foreign.String(), ""); code != http.StatusNotFound {
			t.Errorf("%s reading the other tenant's row = %d %s, want 404", at, code, body)
		}
	}
}

// The refusals of check(), each named by what the mount site says.

func TestAnUnknownVerbRefusesToMount(t *testing.T) {
	bad := spec
	bad.Operations = []httpx.CRUD{"patch"}
	refusesToMount(t, bad, `Operations names "patch"`)
}

func TestADuplicateVerbRefusesToMount(t *testing.T) {
	bad := spec
	bad.Operations = []httpx.CRUD{rest.List, rest.List}
	refusesToMount(t, bad, `Operations names "list" twice`)
}

func TestAGuardSpelledTwiceRefusesToMount(t *testing.T) {
	both := spec
	both.ReadAuth = httpx.SignedIn()
	refusesToMount(t, both, "declares ReadAuth and the permission")

	operator := spec
	operator.Read = ""
	operator.ReadAuth = httpx.OperatorPermission("task:read")
	operator.OperatorRead = true
	refusesToMount(t, operator, "declares ReadAuth with OperatorRead")
}

func TestAPublicSpecRefusesToMount(t *testing.T) {
	public := spec
	public.Read = ""
	public.ReadAuth = httpx.Public()
	refusesToMount(t, public, "ReadAuth is public")
}

// TestAPlainPermissionInReadAuthRefusesToMountAndOneNeedingAFeatureDoesNot keeps
// one spelling of a plain grant (the Spec's own Read and Write fields) and gives
// the declaration the one thing the shorthand cannot say: the grant *and* the
// plan feature it belongs to.
func TestAPlainPermissionInReadAuthRefusesToMountAndOneNeedingAFeatureDoesNot(t *testing.T) {
	plain := spec
	plain.Read = ""
	plain.ReadAuth = httpx.Permission("task:read")
	refusesToMount(t, plain, "ReadAuth names a plain permission")

	featured := spec
	featured.Read = ""
	featured.ReadAuth = httpx.Permission("task:read").Needing("projects")
	api, router, _ := mountPlain(t, featured, caller{})
	op := recorded(t, api, "tasks-task-list")
	if op == nil {
		t.Fatal("the list route is gone")
	}
	auth, _ := op.Extensions[httpx.AuthExtension].(httpx.Auth)
	if auth.String() != "permission task:read" || auth.Feature() != "projects" {
		t.Errorf("the route declares %s needing %q, want the grant and the feature the Spec named", auth, auth.Feature())
	}
	if code, body := call(t, router, http.MethodGet, "/api/v1/tasks/task", ""); code != http.StatusOK {
		t.Errorf("a caller the grant and the plan both admit read the list at %d %s", code, body)
	}
}

// TestASignedInWriteGuardRefusesTheGenericWrites closes the gap the brief opens:
// a guard that decides nothing about the row cannot guard a write whose row the
// caller names, because the only thing between the id and the UPDATE is a tenant
// recheck. The write that names no row — a create — mounts, and admits a member
// who holds nothing.
func TestASignedInWriteGuardRefusesTheGenericWrites(t *testing.T) {
	for _, verb := range []httpx.CRUD{rest.Update, rest.Delete} {
		stale := spec
		stale.Operations = []httpx.CRUD{rest.List, rest.Read, verb}
		stale.Write = ""
		stale.WriteAuth = httpx.SignedIn()
		refusesToMount(t, stale, "no grant")
	}

	create := spec
	create.Operations = []httpx.CRUD{rest.Create}
	create.Write = ""
	create.WriteAuth = httpx.SignedIn()
	_, router, admin := mountAs(t, create, member{})
	if _, err := admin.ExecContext(t.Context(), "ALTER TABLE rest_tasks DROP CONSTRAINT rest_tasks_tenant_id_title_key"); err != nil {
		t.Fatal(err)
	}
	if code, body := call(t, router, http.MethodPost, "/api/v1/tasks/task", `{"title":"any member may file one"}`); code != http.StatusCreated {
		t.Fatalf("a member holding no grant created a row at %d %s, want 201", code, body)
	}
}

func TestImmutableWithoutAWriteRouteRefusesToMount(t *testing.T) {
	nothing := spec
	nothing.Operations = []httpx.CRUD{rest.List, rest.Read}
	nothing.Immutable = []string{"priority"}
	refusesToMount(t, nothing, "mounts neither create nor update")
}

// TestTheOperationSetTravelsToTheRegisteredResource is the one line the screens
// read: what Mount mounted is what the resource says it offers, so a generated
// page and the catalog drawn beside it cannot offer a verb the router does not.
func TestTheOperationSetTravelsToTheRegisteredResource(t *testing.T) {
	only := spec
	only.Operations = []httpx.CRUD{rest.Delete, rest.Read, rest.List} // the Spec's order says nothing about the document's
	api, _, _ := mount(t, only)
	res := api.Resources()[0]
	if got, want := res.OperationWords(), []string{"list", "read", "delete"}; !slices.Equal(got, want) {
		t.Errorf("OperationWords = %v, want %v in the canonical order", got, want)
	}
	for _, c := range []httpx.CRUD{rest.List, rest.Read, rest.Delete} {
		if !res.Offers(c) {
			t.Errorf("the resource denies it offers %s, which the Spec mounted", c)
		}
	}
	for _, c := range []httpx.CRUD{rest.Create, rest.Update} {
		if res.Offers(c) {
			t.Errorf("the resource says it offers %s, which the Spec withheld", c)
		}
	}
}
