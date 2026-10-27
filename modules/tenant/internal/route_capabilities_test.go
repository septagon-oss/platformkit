package internal_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts/tenanttest"
	"github.com/septagon-oss/platformkit/modules/tenant/internal"
	"github.com/septagon-oss/platformkit/modules/user"
)

type routeDirectory struct{ contracts.Directory }
type routeCreator struct{ contracts.Creator }
type routeLifecycle struct{ contracts.Lifecycle }
type routeHosts struct{ contracts.HostManager }
type routeLocale struct{ contracts.LocaleSetter }
type routeOIDC struct{ contracts.OIDCWriter }
type routeSAML struct{ contracts.SAMLWriter }
type routeHostLookup struct{ contracts.HostLookup }
type operatorAccess struct{}

func (operatorAccess) Allowed(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) {
	return true, nil
}

type routeInviter struct{ target *uuid.UUID }

func (i routeInviter) Invite(_ context.Context, _ db.Tx[db.System], target uuid.UUID, email, _ string) error {
	*i.target = target
	return nil
}

func TestTenantRoutesUseDistinctNarrowCapabilities(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, auth.Migrations)
	svc := internal.NewService(nil, tenanttest.InstallationLanguages(), "")
	installed(t, conn, svc)
	api, router := httpx.New(httpx.Options{Cache: cache.Memory("pkit"), Conn: conn, PublicHost: "ops.example.com", Installation: "ops.example.com",
		Tenants: routeHostLookup{svc}, Authorize: operatorAccess{}, Log: slog.New(slog.DiscardHandler),
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: uuid.New()}, true, nil
		}})
	var invited uuid.UUID
	internal.RegisterRoutes(api.Surfaces("tenant").Ops, internal.RouteServices{
		Directory: routeDirectory{svc}, Creation: routeCreator{svc}, Lifecycle: routeLifecycle{svc},
		Hosts: routeHosts{svc}, Locale: routeLocale{svc}, OIDC: routeOIDC{svc}, SAML: routeSAML{svc},
	}, routeInviter{&invited}, api.SystemToken())
	post := func(path, body string) *httptest.ResponseRecorder {
		return tenantRouteRequest(t, router, "POST", path, body, 200)
	}
	created := tenantRouteRequest(t, router, "POST", "", `{"slug":"narrow","name":"Narrow","host":"narrow.example.com"}`, 201)
	var person contracts.Tenant
	if err := json.Unmarshal(created.Body.Bytes(), &person); err != nil {
		t.Fatal(err)
	}
	at := "/" + person.ID.String()
	tenantRouteRequest(t, router, "GET", "", "", 200)
	tenantRouteRequest(t, router, "GET", at, "", 200)
	tenantRouteRequest(t, router, "POST", at+"/invite", `{"email":"ada@example.test"}`, 201)
	if invited != person.ID {
		t.Fatalf("invited tenant = %s", invited)
	}
	post(at+"/suspend", "{}")
	post(at+"/reactivate", "{}")
	post(at+"/rename", `{"name":"Renamed"}`)
	tenantRouteRequest(t, router, "POST", at+"/hosts", `{"host":"alias.example.com"}`, 201)
	tenantRouteRequest(t, router, "DELETE", at+"/hosts/alias.example.com", "", 200)
	post(at+"/locale", `{"default":"en","supported":["en"]}`)
	for _, provider := range []struct {
		name     string
		settings any
	}{{"oidc", acmePeople}, {"saml", acmeFederation}} {
		body, _ := json.Marshal(provider.settings)
		post(at+"/"+provider.name, string(body))
		post(at+"/"+provider.name+"/clear", "{}")
	}
	post(at+"/delete", `{"confirm":"narrow"}`)
	tenantRouteRequest(t, router, "GET", at, "", 404)
}

func tenantRouteRequest(t *testing.T, router http.Handler, method, path, body string, want int) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "http://ops.example.com/api/v1/ops/tenant/tenants"+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
	result := httptest.NewRecorder()
	router.ServeHTTP(result, req)
	if result.Code != want {
		t.Fatalf("%s %s = %d %s, want %d", method, path, result.Code, result.Body.String(), want)
	}
	return result
}
