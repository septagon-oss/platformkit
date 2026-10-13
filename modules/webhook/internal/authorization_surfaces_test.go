package internal_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts/tenanttest"
	"github.com/septagon-oss/platformkit/modules/webhook"
)

func TestWebhookRoutesAndResourceClosuresRefuseAnActorWithoutGrants(t *testing.T) {
	e := newEnv(t, false, nil, nil)
	tenants, _, _ := tenanttest.Installed()
	_, err := tenants.Create(t.Context(), db.Tx[db.System]{}, tenantcontracts.NewTenant{
		Slug: "webhook-reader", Name: "Webhook reader", Host: "webhook.test",
	})
	if err != nil {
		t.Fatal(err)
	}
	principal := tenancy.Principal{UserID: uuid.New()}
	api, router := httpx.New(httpx.Options{
		Cache: cache.Memory("webhook-refusals"), Conn: e.conn, Tenants: tenants,
		PublicHost: "webhook.test", Authorize: catalogueDenied{}, Log: slog.New(slog.DiscardHandler),
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return principal, true, nil
		},
	})
	webhook.New(webhook.Deps{SecretKey: make([]byte, 32), Catalogue: catalogue(emitted)}).Routes(api.Surfaces("webhook"))
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, operation := range api.Recorded() {
		if !strings.HasPrefix(operation.Path, "/api/v1/webhook/") {
			continue
		}
		checked++
		path := strings.ReplaceAll(operation.Path, "{id}", uuid.NewString())
		t.Run(operation.OperationID, func(t *testing.T) {
			req := httptest.NewRequest(operation.Method, "http://webhook.test"+path, strings.NewReader(`{}`))
			req.Header.Set("Content-Type", "application/json")
			req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if response.Code != http.StatusForbidden {
				t.Errorf("grantless %s %s = %d %s, want 403", operation.Method, path, response.Code, response.Body.String())
			}
		})
	}
	if checked == 0 {
		t.Fatal("no webhook route was exercised")
	}
	ctx := tenancy.WithPrincipal(tenancy.WithTenant(t.Context(), acme), principal)
	for _, resource := range api.Resources() {
		t.Run(resource.Entity+" closures", func(t *testing.T) {
			if _, err := resource.Count(ctx); err == nil || !strings.Contains(err.Error(), httpx.CodeDenied) {
				t.Errorf("grantless count: %v, want AUTH_DENIED", err)
			}
			if _, _, err := resource.List(ctx, crud.Query{}); err == nil || !strings.Contains(err.Error(), httpx.CodeDenied) {
				t.Errorf("grantless list: %v, want AUTH_DENIED", err)
			}
			if _, err := resource.Get(ctx, uuid.New()); err == nil || !strings.Contains(err.Error(), httpx.CodeDenied) {
				t.Errorf("grantless get: %v, want AUTH_DENIED", err)
			}
			if resource.Update != nil {
				if _, err := resource.Update(ctx, uuid.New(), map[string]any{"name": "changed"}); err == nil || !strings.Contains(err.Error(), httpx.CodeDenied) {
					t.Errorf("grantless update: %v, want AUTH_DENIED", err)
				}
			}
			if resource.Delete != nil {
				if err := resource.Delete(ctx, uuid.New()); err == nil || !strings.Contains(err.Error(), httpx.CodeDenied) {
					t.Errorf("grantless delete: %v, want AUTH_DENIED", err)
				}
			}
			if got := resource.CommandsFor(ctx); len(got) != 0 {
				t.Errorf("grantless actor can discover %d commands", len(got))
			}
		})
	}
}
