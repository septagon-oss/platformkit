package httpx_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

const host = "acme.test"

// fixture is every collaborator httpx.New needs, with the knobs a test turns:
// who is calling, whether the authorizer says yes, no, or "cannot say", and
// whether the host resolves, does not exist, or cannot be looked up.
type fixture struct {
	tenant     tenancy.Tenant
	resolveNil bool
	loadErr    error
	loads      atomic.Int32
	principal  *tenancy.Principal
	allow      bool
	authErr    error
	// asked counts the calls to Allowed, which is how a test says the
	// authorizer was never reached rather than that it said no.
	asked atomic.Int32
	app   *db.Conn
	// exec runs one DDL statement as the schema owner, which is all a test
	// wants the admin connection for.
	exec func(query string)

	// authnErr makes the identity hook fail, which is an outage rather than an
	// anonymous caller.
	authnErr error
}

func (f *fixture) ByHost(_ context.Context, _ db.Tx[db.System], h string) (tenancy.Tenant, error) {
	f.loads.Add(1)
	if f.loadErr != nil {
		return tenancy.Tenant{}, f.loadErr
	}
	if h != host {
		return tenancy.Tenant{}, tenancy.ErrNoSuchHost
	}
	if f.resolveNil {
		return tenancy.Tenant{}, nil
	}
	return f.tenant, nil
}

func (f *fixture) Allowed(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) {
	f.asked.Add(1)
	return f.allow, f.authErr
}

// authenticate is the identity hook in its E3 shape: it runs after the host has
// resolved, inside that tenant's transaction, so a real implementation looks a
// session up under row-level security. This one answers from a field, and takes
// the transaction only to prove it was given one.
func (f *fixture) authenticate(_ context.Context, tx db.Tx[db.Tenant], _ *http.Request) (tenancy.Principal, bool, error) {
	if f.authnErr != nil {
		return tenancy.Principal{}, false, f.authnErr
	}
	if f.principal == nil {
		return tenancy.Principal{}, false, nil
	}
	if db.TenantOf(tx).ID != f.tenant.ID {
		return tenancy.Principal{}, false, errors.New("the hook was handed another tenant's transaction")
	}
	return *f.principal, true, nil
}

// signedIn makes the caller somebody. There is no tenant to give them: the hook
// runs inside one tenant's transaction, so the principal it produces belongs to
// that tenant by construction.
func (f *fixture) signedIn() {
	f.principal = &tenancy.Principal{UserID: uuid.New()}
}

func setup(t *testing.T) (*httpx.API, *chi.Mux, *fixture) {
	return setupWith(t, false)
}

func setupWith(t *testing.T, docs bool) (*httpx.API, *chi.Mux, *fixture) {
	t.Helper()
	admin, app := dbtest.Schema(t)
	f := &fixture{
		tenant: tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"},
		app:    app,
	}
	f.exec = func(query string) {
		t.Helper()
		if _, err := admin.ExecContext(t.Context(), query); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
	}
	api, router := httpx.New(httpx.Options{
		PublicHost:   host,
		Docs:         docs,
		Tenants:      f,
		Conn:         app,
		Authorize:    f,
		Authenticate: f.authenticate,
		Log:          slog.New(slog.DiscardHandler),
	})
	return api, router, f
}

type body struct {
	Body struct {
		Tenant string `json:"tenant"`
	}
}

func ok(_ context.Context, _ *struct{}) (*body, error) { return &body{}, nil }

func get(t *testing.T, r http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	return request(t, r, http.MethodGet, path, host)
}

func request(t *testing.T, r http.Handler, method, path, h string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "http://"+h+path, nil)
	// The kernel asks the identity hook only about a request that presents
	// something to recognise, so that a request carrying no credential — a
	// liveness probe, an anonymous read — never opens a transaction to be told
	// it is anonymous. Every request in this file that expects to be recognised
	// therefore carries one.
	// The session cookie is the one credential shape the kernel recognises, so
	// a test that wants its identity hook called presents one. The value is not
	// read: this file's hook answers without looking. See kit/httpx.credentialed.
	req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// TestPermissionIsCheckedAgainstTheAuthorizer walks every answer a Permission
// declaration can produce.
func TestPermissionIsCheckedAgainstTheAuthorizer(t *testing.T) {
	api, router, f := setup(t)
	httpx.Register(api, huma.Operation{
		OperationID: "read-widget", Method: http.MethodGet, Path: "/widgets",
	}, httpx.Permission("widget:read"), ok)

	if got := get(t, router, "/widgets").Code; got != http.StatusForbidden {
		t.Errorf("anonymous caller got %d, want 403", got)
	}

	// An identity hook that could not answer is an outage, not an anonymous
	// caller: a session store nobody can read must not read as "not signed in".
	f.authnErr = errors.New("the session store is unreachable")
	if got := get(t, router, "/widgets").Code; got != http.StatusInternalServerError {
		t.Errorf("a failing identity hook got %d, want 500", got)
	}
	f.authnErr = nil

	f.signedIn()
	f.allow = false
	if got := get(t, router, "/widgets").Code; got != http.StatusForbidden {
		t.Errorf("denied caller got %d, want 403", got)
	}

	f.allow = true
	if got := get(t, router, "/widgets").Code; got != http.StatusOK {
		t.Errorf("allowed caller got %d, want 200", got)
	}

	// An authorizer that cannot answer is not an authorizer that said no.
	f.authErr = errors.New("policy store unreachable")
	res := get(t, router, "/widgets")
	if res.Code != http.StatusServiceUnavailable {
		t.Errorf("unavailable authorizer got %d, want 503", res.Code)
	}
	if res.Header().Get("Retry-After") == "" {
		t.Error("a 503 from an outage carries no Retry-After")
	}
}

// TestPublicServesAnonymously, and TestSignedIn does not.
func TestPublicServesAnonymously(t *testing.T) {
	api, router, _ := setup(t)
	httpx.Register(api, huma.Operation{
		OperationID: "public-thing", Method: http.MethodGet, Path: "/public",
	}, httpx.Public(), ok)

	if got := get(t, router, "/public").Code; got != http.StatusOK {
		t.Errorf("public operation got %d, want 200", got)
	}
}

func TestSignedInRequiresAPrincipalOfThisTenant(t *testing.T) {
	api, router, f := setup(t)
	httpx.Register(api, huma.Operation{
		OperationID: "me", Method: http.MethodGet, Path: "/me",
	}, httpx.SignedIn(), ok)

	if got := get(t, router, "/me").Code; got != http.StatusForbidden {
		t.Errorf("anonymous caller got %d, want 403", got)
	}
	f.signedIn()
	if got := get(t, router, "/me").Code; got != http.StatusOK {
		t.Errorf("signed-in caller got %d, want 200", got)
	}
}

// TestAnUnknownHostIsNotAnOutage. The two failures a loader can report are
// different facts and must not look alike: no such site is 404, "I could not
// tell" is 503, and a probe reaching the pod by address still gets its answer.
func TestAnUnknownHostIsNotAnOutage(t *testing.T) {
	api, router, f := setup(t)
	f.signedIn()
	f.allow = true
	httpx.Register(api, huma.Operation{
		OperationID: "read-widget", Method: http.MethodGet, Path: "/widgets",
	}, httpx.Permission("widget:read"), ok)
	httpx.Register(api, huma.Operation{
		OperationID: "public-thing", Method: http.MethodGet, Path: "/public",
	}, httpx.Public(), ok)

	res := request(t, router, http.MethodGet, "/widgets", "nobody.test")
	if res.Code != http.StatusNotFound {
		t.Errorf("unknown host got %d, want 404", res.Code)
	}
	if ct := res.Header().Get("Content-Type"); !strings.Contains(ct, "problem+json") {
		t.Errorf("Content-Type is %q, want a problem document", ct)
	}
	if got := request(t, router, http.MethodGet, "/public", "10.0.0.7:8080").Code; got != http.StatusOK {
		t.Errorf("public operation at an address host got %d, want 200", got)
	}

	f.loadErr = errors.New("dial tcp: connection refused")
	res = request(t, router, http.MethodGet, "/widgets", "elsewhere.test")
	if res.Code != http.StatusServiceUnavailable {
		t.Errorf("a loader outage got %d, want 503", res.Code)
	}
	if res.Header().Get("Retry-After") == "" {
		t.Error("a 503 from an outage carries no Retry-After")
	}

	// A loader that answers with tenancy.Tenant{} and no error has resolved
	// nothing and does not know it: that is a broken loader, not a missing site.
	f.loadErr = nil
	f.resolveNil = true
	if got := get(t, router, "/widgets").Code; got != http.StatusServiceUnavailable {
		t.Errorf("a zero tenant served %d, want 503", got)
	}
}

// TestAResolvedHostIsRememberedForAWhile: one query per host per interval, and
// only for hosts that resolved — a failure is asked again.
func TestAResolvedHostIsRememberedForAWhile(t *testing.T) {
	api, router, f := setup(t)
	httpx.Register(api, huma.Operation{
		OperationID: "public-thing", Method: http.MethodGet, Path: "/public",
	}, httpx.Public(), ok)

	for range 3 {
		get(t, router, "/public")
	}
	if n := f.loads.Load(); n != 1 {
		t.Errorf("%d loads for three requests to one host, want 1", n)
	}
	for range 3 {
		request(t, router, http.MethodGet, "/public", "nobody.test")
	}
	if n := f.loads.Load(); n != 4 {
		t.Errorf("%d loads after three unknown hosts, want 4: a failure must not be remembered", n)
	}
}

// TestHandlerSeesTheTenantTransactionAndItRollsBackOnFailure is the point of
// the middleware chain: a handler never opens a transaction, and a failed
// response leaves nothing behind.
func TestHandlerSeesTheTenantTransactionAndItRollsBackOnFailure(t *testing.T) {
	api, router, f := setup(t)
	f.exec(`CREATE TABLE notes (id serial PRIMARY KEY, tenant_id uuid NOT NULL, body text NOT NULL)`)

	write := func(fail bool) func(context.Context, *struct{}) (*body, error) {
		return func(ctx context.Context, _ *struct{}) (*body, error) {
			tx, present := httpx.TxFrom(ctx)
			if !present {
				return nil, errors.New("no transaction")
			}
			if got := db.TenantOf(tx); got != f.tenant {
				t.Errorf("transaction is scoped to %v, want %v", got, f.tenant)
			}
			if err := tx.DB().Exec("INSERT INTO notes (tenant_id, body) VALUES (?, ?)",
				f.tenant.ID.String(), "written").Error; err != nil {
				return nil, err
			}
			if fail {
				return nil, errors.New("the handler changed its mind")
			}
			out := &body{}
			out.Body.Tenant = db.TenantOf(tx).Slug
			return out, nil
		}
	}
	httpx.Register(api, huma.Operation{
		OperationID: "fail", Method: http.MethodPost, Path: "/notes/fail",
	}, httpx.Public(), write(true))
	httpx.Register(api, huma.Operation{
		OperationID: "keep", Method: http.MethodPost, Path: "/notes/keep",
	}, httpx.Public(), write(false))

	if got := request(t, router, http.MethodPost, "/notes/fail", host).Code; got != http.StatusInternalServerError {
		t.Fatalf("failing handler got %d, want 500", got)
	}
	if n := notes(t, f); n != 0 {
		t.Errorf("%d notes survived a 500, want 0", n)
	}

	if got := request(t, router, http.MethodPost, "/notes/keep", host).Code; got != http.StatusOK {
		t.Fatalf("succeeding handler got %d, want 200", got)
	}
	if n := notes(t, f); n != 1 {
		t.Errorf("%d notes after a 200, want 1", n)
	}
}

// TestAResponseIsHeldUntilTheTransactionCommits. A DEFERRABLE INITIALLY
// DEFERRED constraint is checked at COMMIT, which is after the handler returned
// its 200: without buffering, the caller is told the write succeeded and it did
// not.
func TestAResponseIsHeldUntilTheTransactionCommits(t *testing.T) {
	api, router, f := setup(t)
	f.exec(`CREATE TABLE notes (id serial PRIMARY KEY, tenant_id uuid NOT NULL, body text NOT NULL,
		CONSTRAINT notes_unique UNIQUE (tenant_id, body) DEFERRABLE INITIALLY DEFERRED)`)

	httpx.Register(api, huma.Operation{
		OperationID: "twice", Method: http.MethodPost, Path: "/notes/twice",
	}, httpx.Public(), func(ctx context.Context, _ *struct{}) (*body, error) {
		tx, _ := httpx.TxFrom(ctx)
		for range 2 {
			if err := tx.DB().Exec("INSERT INTO notes (tenant_id, body) VALUES (?, ?)",
				f.tenant.ID.String(), "same").Error; err != nil {
				return nil, err
			}
		}
		return &body{}, nil
	})

	res := request(t, router, http.MethodPost, "/notes/twice", host)
	if res.Code != http.StatusInternalServerError {
		t.Fatalf("a commit that failed returned %d, want 500", res.Code)
	}
	if ct := res.Header().Get("Content-Type"); !strings.Contains(ct, "problem+json") {
		t.Errorf("Content-Type is %q, want a problem document", ct)
	}
	if n := notes(t, f); n != 0 {
		t.Errorf("%d notes after a failed commit, want 0", n)
	}
}

// TestAPanicIsOneFailedRequest, not a dead process and not a committed
// half-transaction: the recovery is outside the transaction on purpose.
func TestAPanicIsOneFailedRequest(t *testing.T) {
	api, router, f := setup(t)
	f.exec(`CREATE TABLE notes (id serial PRIMARY KEY, tenant_id uuid NOT NULL, body text NOT NULL)`)

	httpx.Register(api, huma.Operation{
		OperationID: "boom", Method: http.MethodPost, Path: "/notes/boom",
	}, httpx.Public(), func(ctx context.Context, _ *struct{}) (*body, error) {
		tx, _ := httpx.TxFrom(ctx)
		if err := tx.DB().Exec("INSERT INTO notes (tenant_id, body) VALUES (?, ?)",
			f.tenant.ID.String(), "written").Error; err != nil {
			return nil, err
		}
		panic("the handler exploded")
	})

	res := request(t, router, http.MethodPost, "/notes/boom", host)
	if res.Code != http.StatusInternalServerError {
		t.Fatalf("a panicking handler returned %d, want 500", res.Code)
	}
	if ct := res.Header().Get("Content-Type"); !strings.Contains(ct, "problem+json") {
		t.Errorf("Content-Type is %q, want a problem document", ct)
	}
	if n := notes(t, f); n != 0 {
		t.Errorf("%d notes survived a panic, want 0", n)
	}
}

// TestARequestThatNeverQueriesSurvivesADeadDatabase, and one that queries says
// so honestly. This is why the transaction is opened on first use: a liveness
// probe addressed to a tenant host must not restart the pod during an outage.
func TestARequestThatNeverQueriesSurvivesADeadDatabase(t *testing.T) {
	api, router, f := setup(t)
	httpx.Register(api, huma.Operation{
		OperationID: "quiet", Method: http.MethodGet, Path: "/quiet",
	}, httpx.Public(), ok)
	httpx.Register(api, huma.Operation{
		OperationID: "noisy", Method: http.MethodGet, Path: "/noisy",
	}, httpx.Public(), func(ctx context.Context, _ *struct{}) (*body, error) {
		if _, present := httpx.TxFrom(ctx); !present {
			return nil, errors.New("no transaction")
		}
		return &body{}, nil
	})

	get(t, router, "/quiet") // resolves the host while the database still answers
	if err := f.app.Close(); err != nil {
		t.Fatalf("close the pool: %v", err)
	}

	if got := get(t, router, "/quiet").Code; got != http.StatusOK {
		t.Errorf("a request that queries nothing got %d with the database down, want 200", got)
	}
	if got := get(t, router, "/noisy").Code; got != http.StatusInternalServerError {
		t.Errorf("a request that queries got %d with the database down, want 500", got)
	}
}

// TestEveryRequestCarriesAnId, in the log, in the response header and in the
// problem body's instance, so a report of "I got a 500" is one grep away.
func TestEveryRequestCarriesAnId(t *testing.T) {
	api, router, _ := setup(t)
	httpx.Register(api, huma.Operation{
		OperationID: "public-thing", Method: http.MethodGet, Path: "/public",
	}, httpx.Public(), ok)

	res := get(t, router, "/public")
	if res.Header().Get(httpx.RequestIDHeader) == "" {
		t.Error("no request id in the response")
	}

	req := httptest.NewRequest(http.MethodGet, "http://nobody.test/public", nil)
	req.Header.Set(httpx.RequestIDHeader, "from-the-proxy")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if got := w.Header().Get(httpx.RequestIDHeader); got != "from-the-proxy" {
		t.Errorf("request id = %q, want the caller's", got)
	}

	// A public operation at an unknown host is served, so ask for one that is
	// not: the problem body has to name the request.
	httpx.Register(api, huma.Operation{
		OperationID: "me", Method: http.MethodGet, Path: "/me",
	}, httpx.SignedIn(), ok)
	req = httptest.NewRequest(http.MethodGet, "http://nobody.test/me", nil)
	req.Header.Set(httpx.RequestIDHeader, "trace-42")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if !strings.Contains(w.Body.String(), `"instance":"urn:request:trace-42"`) {
		t.Errorf("the problem body does not carry the request id: %s", w.Body.String())
	}

	// An id a client invented out of newlines is not an id.
	req = httptest.NewRequest(http.MethodGet, "http://"+host+"/public", nil)
	req.Header.Set(httpx.RequestIDHeader, "bad\nid")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if got := w.Header().Get(httpx.RequestIDHeader); strings.Contains(got, "\n") || got == "" {
		t.Errorf("request id = %q, want a generated one", got)
	}
}

// TestHumaOwnRoutesAreRecordedAndDeclared: Recorded is only worth reading if it
// is the whole list, so huma's documentation routes are in it, declared Public
// where they are mounted.
func TestHumaOwnRoutesAreRecordedAndDeclared(t *testing.T) {
	api, router, _ := setupWith(t, true)
	httpx.Register(api, huma.Operation{
		OperationID: "read-widget", Method: http.MethodGet, Path: "/widgets",
	}, httpx.Permission("widget:read"), ok)
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("huma's own routes failed the gate: %v", err)
	}

	var paths []string
	for _, op := range api.Recorded() {
		paths = append(paths, op.Path)
	}
	for _, want := range []string{"/openapi.json", "/openapi.yaml", "/docs", "/schemas/{schema}"} {
		if !strings.Contains(strings.Join(paths, " "), want) {
			t.Errorf("%s is not recorded: %v", want, paths)
		}
	}

	// The declaration a reviewer reads in the document is the value the
	// middleware enforced, because there is only one of it.
	doc := get(t, router, "/openapi.json").Body.String()
	const want = `"x-platformkit-auth":{"kind":"permission","permission":"widget:read"}`
	if !strings.Contains(doc, want) {
		t.Errorf("the document does not carry %s", want)
	}

	// With Docs off the routes are simply not mounted; /schemas stays, because
	// response bodies link to it.
	off, offRouter, _ := setup(t)
	if got := get(t, offRouter, "/openapi.json").Code; got != http.StatusNotFound {
		t.Errorf("Docs:false served /openapi.json with %d, want 404", got)
	}
	if err := off.ValidateDeclarations(); err != nil {
		t.Errorf("Docs:false failed the gate: %v", err)
	}
}

// TestRequiredListsWhatTheRoutesAsk, which is what kit/app checks against the
// modules' manifests — the name and the kind, because the two have to agree.
func TestRequiredListsWhatTheRoutesAsk(t *testing.T) {
	api, _, _ := setup(t)
	httpx.Register(api, huma.Operation{
		OperationID: "read-widget", Method: http.MethodGet, Path: "/widgets",
	}, httpx.Permission("widget:read"), ok)
	httpx.Register(api, huma.Operation{
		OperationID: "create-widget", Method: http.MethodPost, Path: "/widgets",
	}, httpx.Permission("widget:read"), ok)
	httpx.Register(api, huma.Operation{
		OperationID: "operate-widget", Method: http.MethodPost, Path: "/widgets/all",
	}, httpx.OperatorPermission("widget:operate"), ok)
	httpx.Register(api, huma.Operation{
		OperationID: "public-thing", Method: http.MethodGet, Path: "/public",
	}, httpx.Public(), ok)

	want := []tenancy.Grant{{Permission: "widget:operate", Operator: true}, {Permission: "widget:read"}}
	if got := api.Required(); !slices.Equal(got, want) {
		t.Errorf("Required = %v, want %v", got, want)
	}

	// Permissions is the other list: what the modules define, which the kernel
	// hands over before any route is registered. It is empty until it is set,
	// and it is not derived from the routes.
	if got := api.Permissions(); len(got) != 0 {
		t.Errorf("Permissions before Declare = %v, want nothing", got)
	}
	api.Declare([]tenancy.Grant{{Permission: "widget:read"}, {Permission: "widget:operate", Operator: true}})
	if got := api.Permissions(); !slices.Equal(got, want) {
		t.Errorf("Permissions = %v, want %v", got, want)
	}
}

// TestPermissionRejectsAMalformedToken at the registration site, where the
// mistake is, rather than as a permission nobody can ever hold.
func TestPermissionRejectsAMalformedToken(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("httpx.Permission accepted a malformed token")
		}
	}()
	httpx.Permission("Widgets.Read")
}

// TestStaticFilesAreNotOperations: a file has no handler to authorize and no
// tenant transaction to open, so it is served by the router and never recorded.
func TestStaticFilesAreNotOperations(t *testing.T) {
	api, router, _ := setup(t)
	api.Static("/assets", fstest.MapFS{"app.css": {Data: []byte("body{}")}})

	res := get(t, router, "/assets/app.css")
	if res.Code != http.StatusOK || res.Body.String() != "body{}" {
		t.Errorf("static file = %d %q", res.Code, res.Body.String())
	}
	if err := api.ValidateDeclarations(); err != nil {
		t.Errorf("mounting a file tree needed a declaration: %v", err)
	}
}

func notes(t *testing.T, f *fixture) int {
	t.Helper()
	var n int
	err := db.Run(tenancy.WithTenant(t.Context(), f.tenant), f.app, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw("SELECT count(*) FROM notes").Row().Scan(&n)
	})
	if err != nil {
		t.Fatalf("count notes: %v", err)
	}
	return n
}

// TestAFailedCommitDoesNotKeepTheHandlersHeaders. The handler answered 201 with
// a Location, and then its transaction did not commit: the 500 that replaces
// the response must not point at a resource that does not exist.
func TestAFailedCommitDoesNotKeepTheHandlersHeaders(t *testing.T) {
	api, router, f := setup(t)
	f.exec(`CREATE TABLE notes (id serial PRIMARY KEY, tenant_id uuid NOT NULL, body text NOT NULL,
		CONSTRAINT notes_unique UNIQUE (tenant_id, body) DEFERRABLE INITIALLY DEFERRED)`)

	type created struct {
		Location string `header:"Location"`
		Body     struct{}
	}
	httpx.Register(api, huma.Operation{
		OperationID: "create-note", Method: http.MethodPost, Path: "/notes",
		DefaultStatus: http.StatusCreated,
	}, httpx.Public(), func(ctx context.Context, _ *struct{}) (*created, error) {
		tx, _ := httpx.TxFrom(ctx)
		for range 2 {
			if err := tx.DB().Exec("INSERT INTO notes (tenant_id, body) VALUES (?, ?)",
				f.tenant.ID.String(), "same").Error; err != nil {
				return nil, err
			}
		}
		return &created{Location: "/notes/1"}, nil
	})

	res := request(t, router, http.MethodPost, "/notes", host)
	if res.Code != http.StatusInternalServerError {
		t.Fatalf("a commit that failed returned %d, want 500", res.Code)
	}
	if loc := res.Header().Get("Location"); loc != "" {
		t.Errorf("the 500 still carries Location: %q", loc)
	}
	if id := res.Header().Get(httpx.RequestIDHeader); id == "" {
		t.Error("the reset dropped the request id, which was set before the handler ran")
	}
}

// TestALargeResponseStopsBeingHeld. Holding a response is what makes
// "committed before 200" possible, and it costs a copy; past the limit the copy
// is the wrong trade, so the buffer sends what it has and streams the rest —
// with the same consequence a Flush has.
func TestALargeResponseStopsBeingHeld(t *testing.T) {
	api, router, _ := setup(t)
	type blob struct {
		Body []byte
	}
	httpx.Register(api, huma.Operation{
		OperationID: "big", Method: http.MethodGet, Path: "/big",
	}, httpx.Public(), func(context.Context, *struct{}) (*blob, error) {
		return &blob{Body: bytes.Repeat([]byte("x"), 3<<20)}, nil
	})
	res := get(t, router, "/big")
	if res.Code != http.StatusOK || res.Body.Len() != 3<<20 {
		t.Errorf("a 3 MB response = %d, %d bytes", res.Code, res.Body.Len())
	}
}

// TestAStreamedResponseCommits: huma never sets a status on the streaming path,
// and a status of zero is not a success — but a response already on the wire is
// a 200 whatever this package thinks, so it commits.
func TestAStreamedResponseCommits(t *testing.T) {
	api, router, f := setup(t)
	f.exec(`CREATE TABLE notes (id serial PRIMARY KEY, tenant_id uuid NOT NULL, body text NOT NULL)`)

	write := func(ctx context.Context) error {
		tx, ok := httpx.TxFrom(ctx)
		if !ok {
			return errors.New("no transaction")
		}
		return tx.DB().Exec("INSERT INTO notes (tenant_id, body) VALUES (?, ?)", f.tenant.ID.String(), "streamed").Error
	}
	httpx.Register(api, huma.Operation{
		OperationID: "flushed", Method: http.MethodGet, Path: "/flushed",
	}, httpx.Public(), func(ctx context.Context, _ *struct{}) (*huma.StreamResponse, error) {
		if err := write(ctx); err != nil {
			return nil, err
		}
		return &huma.StreamResponse{Body: func(hctx huma.Context) {
			w := hctx.BodyWriter()
			_, _ = w.Write([]byte("chunk"))
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
		}}, nil
	})

	res := get(t, router, "/flushed")
	if res.Code != http.StatusOK || res.Body.String() != "chunk" {
		t.Fatalf("streamed response = %d %q", res.Code, res.Body.String())
	}
	if n := notes(t, f); n != 1 {
		t.Errorf("%d notes after a streamed response, want 1: the transaction has to commit", n)
	}
}

// TestAResponseNobodyDecidedIsNotCommitted is the other half of the same rule.
// A handler that wrote no status and never began the response has decided
// nothing, and a transaction nobody decided about is a 500, not a 200.
func TestAResponseNobodyDecidedIsNotCommitted(t *testing.T) {
	api, router, f := setup(t)
	f.exec(`CREATE TABLE notes (id serial PRIMARY KEY, tenant_id uuid NOT NULL, body text NOT NULL)`)

	httpx.Register(api, huma.Operation{
		OperationID: "undecided", Method: http.MethodGet, Path: "/undecided",
	}, httpx.Public(), func(ctx context.Context, _ *struct{}) (*huma.StreamResponse, error) {
		tx, _ := httpx.TxFrom(ctx)
		if err := tx.DB().Exec("INSERT INTO notes (tenant_id, body) VALUES (?, ?)",
			f.tenant.ID.String(), "undecided").Error; err != nil {
			return nil, err
		}
		return &huma.StreamResponse{Body: func(huma.Context) {}}, nil
	})

	res := get(t, router, "/undecided")
	if res.Code != http.StatusInternalServerError {
		t.Errorf("an undecided response = %d, want 500", res.Code)
	}
	if n := notes(t, f); n != 0 {
		t.Errorf("%d notes after an undecided response, want 0", n)
	}
}

// TestInvalidateHostForgetsTheResolution, so renaming a site takes effect now
// rather than within the cache's half minute.
func TestInvalidateHostForgetsTheResolution(t *testing.T) {
	api, router, f := setup(t)
	httpx.Register(api, huma.Operation{
		OperationID: "ping", Method: http.MethodGet, Path: "/ping",
	}, httpx.Public(), ok)

	for range 3 {
		if code := get(t, router, "/ping").Code; code != http.StatusOK {
			t.Fatalf("GET /ping = %d", code)
		}
	}
	if n := f.loads.Load(); n != 1 {
		t.Fatalf("the loader was asked %d times for three requests, want 1", n)
	}
	api.InvalidateHost(strings.ToUpper(host) + ":8080")
	if code := get(t, router, "/ping").Code; code != http.StatusOK {
		t.Fatalf("GET /ping after invalidation = %d", code)
	}
	if n := f.loads.Load(); n != 2 {
		t.Errorf("the loader was asked %d times, want 2: the invalidation did not take", n)
	}
}

// TestAnOperatorRouteIsRefusedBeforeTheAuthorizer is the kernel's own proof of
// the rule modules/tenant's five control-plane routes depend on, and it is here
// rather than only in apps/ because the rule is the kernel's.
//
// The control plane is served at every tenant's host, so an operator route is
// reachable with a customer's own credentials, and a customer's administrator
// holds the wildcard in their own tenant by construction. If the check ran
// after the authorizer, that wildcard would answer a question about every
// tenant. So the authorizer here always says yes — the strongest answer it can
// give — and the refusal has to come anyway, from the tenant flag alone.
//
// The counter is the other half: it is not enough that the answer is 403, the
// question must never have been asked. Deleting the four lines at the refusal
// in middleware.go turns both halves red.
func TestAnOperatorRouteIsRefusedBeforeTheAuthorizer(t *testing.T) {
	api, router, f := setup(t)
	httpx.Register(api, huma.Operation{
		OperationID: "operate-widget", Method: http.MethodGet, Path: "/widgets/all",
	}, httpx.OperatorPermission("widget:operate"), ok)
	// An authorizer that grants everything, so nothing here can be mistaken for
	// the roles table refusing.
	f.allow, f.authErr = true, nil
	f.signedIn()

	res := get(t, router, "/widgets/all")
	if res.Code != http.StatusForbidden {
		t.Errorf("a customer's tenant reached an operator route: %d %s", res.Code, res.Body)
	}
	if !strings.Contains(res.Body.String(), "AUTH_NOT_OPERATOR") {
		t.Errorf("the refusal does not say why: %s", res.Body)
	}
	if got := f.asked.Load(); got != 0 {
		t.Errorf("the authorizer was asked %d time(s); the tenant flag decides this before the roles table", got)
	}

	// The operator's own tenant is asked, and gets in. Same route, same
	// authorizer, same caller: the tenant is the whole difference.
	f.tenant.Operator = true
	api.InvalidateHost(host)
	if res := get(t, router, "/widgets/all"); res.Code != http.StatusOK {
		t.Errorf("the operator's own tenant got %d %s", res.Code, res.Body)
	}
	if got := f.asked.Load(); got != 1 {
		t.Errorf("the authorizer was asked %d time(s), want exactly the operator's request", got)
	}
}
