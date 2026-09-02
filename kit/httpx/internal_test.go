package httpx

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

type nothing struct{}

func (nothing) ByHost(context.Context, db.Tx[db.System], string) (tenancy.Tenant, error) {
	return tenancy.Tenant{}, tenancy.ErrNoSuchHost
}
func (nothing) Allowed(context.Context, tenancy.Tenant, string) (bool, error) { return false, nil }

// TestReachingAroundRegisterIsStillRecorded is the negative control, and it has
// to be written from inside the package because from outside there is no longer
// a huma.API to reach: the embedded one was unexported for exactly this reason.
// What the gate rests on is that even this API — the only one that exists —
// records, so an operation registered without a declaration is caught rather
// than invisible. The operation is hidden, so it is in no OpenAPI document.
func TestReachingAroundRegisterIsStillRecorded(t *testing.T) {
	_, app := dbtest.Schema(t)
	a, _ := New(Options{
		Tenants:   nothing{},
		Conn:      app,
		Authorize: nothing{},
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (Principal, bool, error) {
			return Principal{}, false, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	Register(a, huma.Operation{
		OperationID: "declared", Method: http.MethodGet, Path: "/declared",
	}, Public(), func(context.Context, *struct{}) (*struct{}, error) { return nil, nil })
	if err := a.ValidateDeclarations(); err != nil {
		t.Fatalf("a declared API failed validation: %v", err)
	}

	huma.Register(a.api, huma.Operation{
		OperationID: "backdoor", Method: http.MethodGet, Path: "/backdoor", Hidden: true,
	}, func(context.Context, *struct{}) (*struct{}, error) { return nil, nil })

	err := a.ValidateDeclarations()
	if err == nil {
		t.Fatal("ValidateDeclarations passed an undeclared hidden operation")
	}
	if !strings.Contains(err.Error(), "GET /backdoor (backdoor)") {
		t.Errorf("the error does not name the operation: %v", err)
	}

	// The adapter is the other way around Register, and it used to be exported.
	// It is recorded too — but recording is not enforcement: a handler mounted
	// here sits below this package's huma middleware, so it resolves no tenant,
	// opens no transaction and is never authorized, while carrying whatever
	// declaration it likes. That is why there is no accessor for it any more,
	// and this is what it would have bought.
	a.adapter.Handle(&huma.Operation{
		OperationID: "cellar", Method: http.MethodGet, Path: "/cellar", Hidden: true,
	}, func(huma.Context) {})
	if err := a.ValidateDeclarations(); err == nil || !strings.Contains(err.Error(), "GET /cellar (cellar)") {
		t.Errorf("ValidateDeclarations = %v, want the operation mounted on the adapter", err)
	}
}

// TestHostOnlyIsTheKeyEveryLoaderSees, so no TenantLoader has to normalise a
// Host header and none of them can do it differently.
func TestHostOnlyIsTheKeyEveryLoaderSees(t *testing.T) {
	for _, tt := range []struct{ in, want string }{
		{"acme.test", "acme.test"},
		{"acme.test:8080", "acme.test"},
		{"ACME.Test", "acme.test"},
		{"acme.test.", "acme.test"},
		{"ACME.TEST.:443", "acme.test"},
		{"10.0.0.7:8080", "10.0.0.7"},
		{"[::1]:8080", "::1"},
		{"[::1]", "::1"},
		{"[2001:DB8::1]:80", "2001:db8::1"},
	} {
		if got := HostOnly(tt.in); got != tt.want {
			t.Errorf("HostOnly(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
