package httpx_test

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

const host = "acme.test"

// fixture is every collaborator httpx.New needs, with the knobs a test turns:
// who is calling, and whether the authorizer says yes, no, or "cannot say".
type fixture struct {
	tenant     tenancy.Tenant
	resolveNil bool
	principal  *httpx.Principal
	allow      bool
	authErr    error
	admin      *db.Conn
	app        *db.Conn
}

func (f *fixture) ByHost(_ context.Context, h string) (tenancy.Tenant, error) {
	if h != host {
		return tenancy.Tenant{}, errors.New("no tenant at " + h)
	}
	if f.resolveNil {
		return tenancy.Tenant{}, nil
	}
	return f.tenant, nil
}

func (f *fixture) Allowed(context.Context, tenancy.Tenant, string) (bool, error) {
	return f.allow, f.authErr
}

func (f *fixture) authenticate(*http.Request) (httpx.Principal, bool) {
	if f.principal == nil {
		return httpx.Principal{}, false
	}
	return *f.principal, true
}

// signedIn makes the caller a member of the tenant the host resolves to.
func (f *fixture) signedIn() {
	f.principal = &httpx.Principal{UserID: uuid.New(), TenantID: f.tenant.ID}
}

func setup(t *testing.T) (*httpx.API, *chi.Mux, *fixture) {
	t.Helper()
	admin, app := db.TestSchema(t)
	f := &fixture{
		tenant: tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"},
		admin:  admin,
		app:    app,
	}
	api, router := httpx.New(httpx.Options{
		PublicHost:   host,
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
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// TestPermissionIsCheckedAgainstTheAuthorizer walks the four answers a
// Permission declaration can produce.
func TestPermissionIsCheckedAgainstTheAuthorizer(t *testing.T) {
	api, router, f := setup(t)
	httpx.Register(api, huma.Operation{
		OperationID: "read-widget", Method: http.MethodGet, Path: "/widgets",
	}, httpx.Permission("widget:read"), ok)

	if got := get(t, router, "/widgets").Code; got != http.StatusForbidden {
		t.Errorf("anonymous caller got %d, want 403", got)
	}

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

	// A session minted for another tenant is not a session here, and SignedIn
	// asks no Authorizer that could have noticed.
	f.principal = &httpx.Principal{UserID: uuid.New(), TenantID: uuid.New()}
	if got := get(t, router, "/me").Code; got != http.StatusForbidden {
		t.Errorf("caller from another tenant got %d, want 403", got)
	}
}

// TestUnknownHostIsNotFoundUnlessTheOperationIsPublic: a probe reaching the pod
// by address must still get its answer.
func TestUnknownHostIsNotFoundUnlessTheOperationIsPublic(t *testing.T) {
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
}

// TestHandlerSeesTheTenantTransactionAndItRollsBackOnFailure is the whole point
// of the middleware chain: a handler never opens a transaction, and a failed
// response leaves nothing behind.
func TestHandlerSeesTheTenantTransactionAndItRollsBackOnFailure(t *testing.T) {
	api, router, f := setup(t)
	ctx := t.Context()
	if err := f.admin.Exec(ctx, `CREATE TABLE notes (id serial PRIMARY KEY, tenant_id uuid NOT NULL, body text NOT NULL)`); err != nil {
		t.Fatalf("create notes: %v", err)
	}

	write := func(fail bool) func(context.Context, *struct{}) (*body, error) {
		return func(ctx context.Context, _ *struct{}) (*body, error) {
			tx, present := httpx.TxFrom(ctx)
			if !present {
				t.Error("the handler has no transaction")
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

// TestValidateDeclarationsCatchesAHiddenRawRegistration is the negative
// control. httpx.Register cannot produce an undeclared operation, so the only
// way to make one is to go around it — which is exactly what recording at the
// adapter, rather than walking the OpenAPI document, is for: a hidden operation
// is in no document.
func TestValidateDeclarationsCatchesAHiddenRawRegistration(t *testing.T) {
	api, _, _ := setup(t)
	httpx.Register(api, huma.Operation{
		OperationID: "declared", Method: http.MethodGet, Path: "/declared",
	}, httpx.Public(), ok)
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("a declared API failed validation: %v", err)
	}

	huma.Register(api, huma.Operation{
		OperationID: "backdoor", Method: http.MethodGet, Path: "/backdoor", Hidden: true,
	}, ok)

	err := api.ValidateDeclarations()
	if err == nil {
		t.Fatal("ValidateDeclarations passed an undeclared hidden operation")
	}
	if !strings.Contains(err.Error(), "GET /backdoor (backdoor)") {
		t.Errorf("the error does not name the operation: %v", err)
	}

	// huma's own /openapi.json and /docs are mounted before the recorder wraps
	// the adapter, so they are not operations this API declares.
	for _, op := range api.Recorded() {
		if strings.HasPrefix(op.Path, "/openapi") || op.Path == "/docs" {
			t.Errorf("huma's %s %s is recorded; it would need a declaration", op.Method, op.Path)
		}
	}
}

// TestPublicMutationsListsOnlyPublicWrites: the list a reviewer reads first.
func TestPublicMutationsListsOnlyPublicWrites(t *testing.T) {
	api, _, _ := setup(t)
	httpx.Register(api, huma.Operation{
		OperationID: "sign-up", Method: http.MethodPost, Path: "/sign-up",
	}, httpx.Public(), ok)
	httpx.Register(api, huma.Operation{
		OperationID: "create-widget", Method: http.MethodPost, Path: "/widgets",
	}, httpx.Permission("widget:create"), ok)
	httpx.Register(api, huma.Operation{
		OperationID: "list-widgets", Method: http.MethodGet, Path: "/widgets",
	}, httpx.Public(), ok)

	got := api.PublicMutations()
	want := []string{"POST /sign-up"}
	if !slices.Equal(got, want) {
		t.Errorf("PublicMutations = %v, want %v", got, want)
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

// TestTheDocumentCarriesTheDeclaration: the value a reviewer reads in the
// OpenAPI document is the value the middleware enforced above, because there is
// only one of it.
func TestTheDocumentCarriesTheDeclaration(t *testing.T) {
	api, router, _ := setup(t)
	httpx.Register(api, huma.Operation{
		OperationID: "read-widget", Method: http.MethodGet, Path: "/widgets",
	}, httpx.Permission("widget:read"), ok)

	body := get(t, router, "/openapi.json").Body.String()
	const want = `"x-platformkit-auth":{"kind":"permission","permission":"widget:read"}`
	if !strings.Contains(body, want) {
		t.Errorf("the document does not carry %s", want)
	}
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

// TestAZeroTenantIsNotATenant. A resolver that answers with tenancy.Tenant{} and
// no error would otherwise scope the request's transaction to the nil UUID, and
// httpx.Principal{} — which any Authenticate hook can return — would match it.
func TestAZeroTenantIsNotATenant(t *testing.T) {
	api, router, f := setup(t)
	httpx.Register(api, huma.Operation{
		OperationID: "me", Method: http.MethodGet, Path: "/me",
	}, httpx.SignedIn(), ok)
	f.resolveNil = true
	f.principal = &httpx.Principal{}

	if got := get(t, router, "/me").Code; got != http.StatusNotFound {
		t.Errorf("a zero tenant served %d, want 404", got)
	}
}
