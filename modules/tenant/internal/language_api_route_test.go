package internal_test

import (
	"context"
	"log/slog"
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/tenant"
)

type languageRouteTenants struct{}

func (languageRouteTenants) ByHost(context.Context, db.Tx[db.System], string) (tenancy.Tenant, error) {
	return tenancy.Tenant{}, tenancy.ErrNoSuchHost
}

type languageRouteAuthorizer struct{}

func (languageRouteAuthorizer) Allowed(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) {
	return true, nil
}

type languageRouteManager struct{}

func (languageRouteManager) MaySetOwnLocale(context.Context, db.Tx[db.Tenant]) (bool, error) {
	return true, nil
}

func TestTenantLanguagesHaveAnAppAPICommand(t *testing.T) {
	_, conn := dbtest.Schema(t)
	api, _ := httpx.New(httpx.Options{
		Tenants: languageRouteTenants{}, Conn: conn, Authorize: languageRouteAuthorizer{},
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{}, false, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	_, manifest := tenant.Module(tenant.Deps{LocaleManager: languageRouteManager{}, Languages: []string{"en", "pt-PT"}})
	manifest.Routes(api.Surfaces("tenant"))
	route := false
	for _, op := range api.Recorded() {
		if op.Method == http.MethodPut && op.Path == "/api/v1/tenant/languages" {
			if got, _ := op.Extensions[httpx.SurfaceExtension].(string); got != "app" {
				t.Fatalf("language command belongs to the app surface, got %q", got)
			}
			route = true
		}
	}
	if !route {
		t.Error("the tenant's language command has no PUT /api/v1/tenant/languages route")
	}
	for _, resource := range api.Resources() {
		if resource.Module != "tenant" {
			continue
		}
		for _, command := range resource.Commands {
			if command.Endpoint == "/api/v1/tenant/languages" {
				// The door is a replace, so it answers PUT — and the catalog has to say
				// so, because a shell that followed the POST convention this document
				// otherwise implies would be refused at the address it was given.
				if command.Method != http.MethodPut {
					t.Errorf("the tenant language command is reached by %q, its route answers %q",
						command.Method, http.MethodPut)
				}
				return
			}
		}
	}
	t.Error("the tenant language write is absent from resource command discovery")
}
