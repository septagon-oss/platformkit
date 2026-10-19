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
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/scim/contracts"
	"github.com/septagon-oss/platformkit/modules/scim/internal"
)

// The existing SQL fixture supplies users and role definitions. This adapter
// supplies only the host and grant answers needed to exercise the real router.
type provisioningCaller struct{}

func (provisioningCaller) ByHost(context.Context, db.Tx[db.System], string) (tenancy.Tenant, error) {
	return acme, nil
}
func (provisioningCaller) Allowed(_ context.Context, _ tenancy.Tenant, g tenancy.Grant) (bool, error) {
	return g.Permission == contracts.PermissionProvision, nil
}

func provisioningRouter(t *testing.T, w wiring) http.Handler {
	t.Helper()
	api, r := httpx.New(httpx.Options{
		Conn: w.conn, Cache: cache.Memory("pkit"), PublicHost: "acme.test", Tenants: provisioningCaller{}, Authorize: provisioningCaller{}, Log: slog.New(slog.DiscardHandler),
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: uuid.New(), Permissions: []string{contracts.PermissionProvision}}, true, nil
		},
	})
	internal.RegisterRoutes(api.Surfaces("scim"), w.svc)
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatal(err)
	}
	return r
}
func provisioningRequest(r http.Handler, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "http://acme.test"+path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer fixture")
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	r.ServeHTTP(res, req)
	return res
}

func TestProvisioningCannotChooseTheTenantRoleMapping(t *testing.T) {
	w := compose(t, "admin")
	r := provisioningRouter(t, w)
	// Establish reachability with an allowed protocol read and a refused mapping
	// write. Neither control depends on the forbidden write succeeding.
	if res := provisioningRequest(r, "GET", "/scim/v2/Groups", ""); res.Code != 200 {
		t.Fatalf("directory control: %d %s", res.Code, res.Body)
	}
	if res := provisioningRequest(r, "PUT", "/api/v1/scim/group-mappings", `{"group":"People","role":"admin"}`); res.Code != 403 {
		t.Fatalf("mapping control: %d %s", res.Code, res.Body)
	}
	res := provisioningRequest(r, "POST", "/scim/v2/Groups", `{"displayName":"People","role":"admin"}`)
	if res.Code < 400 {
		t.Errorf("provision-only credential chose an admin mapping: %d %s", res.Code, res.Body)
	}
	w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		rows, err := w.svc.GroupMappings(ctx, tx)
		if err != nil {
			return err
		}
		if len(rows) != 0 {
			t.Errorf("refused mapping must leave no bindings, got %+v", rows)
		}
		var count int64
		if err := tx.DB().Table("scim_groups").Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			t.Errorf("refused group write committed %d group(s)", count)
		}
		if names := outbox(t, tx); len(names) != 0 {
			t.Errorf("refused group write emitted %v", names)
		}
		return nil
	})
}
