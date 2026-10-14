package internal_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/user"
	"github.com/septagon-oss/platformkit/ui/screens"
)

func TestAKeyWithoutGrantsDiscoversAndExecutesOnlyItsOwnRetirement(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	router, _, svc, api := mountRecorded(t, conn, auth.OIDC{}, false)
	app.WorkspaceCatalogRoute(func(ctx context.Context, resources []httpx.Resource) (*screens.Catalog, error) {
		doc := screens.Describe(ctx, resources)
		return &doc, nil
	})(api)
	ctx := tenancy.WithTenant(t.Context(), acme)
	setGrants := func(grants []string) {
		t.Helper()
		if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			_, err := svc.SetRole(ctx, tx, "deployer", grants, declared)
			return err
		}); err != nil {
			t.Fatalf("set the holder's grants: %v", err)
		}
	}
	setGrants([]string{contracts.PermissionRoleManage})
	person(t, conn, "ada@acme.localhost", "deployer")
	session := signIn(t, router, "ada@acme.localhost")
	key, _ := mintKey(t, router, session, "Phone")
	setGrants(nil)
	if res := call(t, router, http.MethodGet, "/api/v1/auth/roles", "", bearer(key)); res.Code != http.StatusForbidden {
		t.Fatalf("key after losing its grant = %d %s, want 403", res.Code, res.Body.String())
	}
	res := call(t, router, http.MethodGet, "/api/v1/app/resources", "", bearer(key))
	if res.Code != http.StatusOK {
		t.Fatalf("catalogue = %d %s, want 200", res.Code, res.Body.String())
	}
	var doc screens.Catalog
	if err := json.Unmarshal(res.Body.Bytes(), &doc); err != nil {
		t.Fatalf("read catalogue: %v", err)
	}
	if len(doc.Resources) != 1 {
		t.Fatalf("key without grants sees %d resources, want only its retirement: %s", len(doc.Resources), res.Body.String())
	}
	r := doc.Resources[0]
	if r.Module != "auth" || r.Entity != "token" || r.Operations == nil || len(r.Operations) != 0 || r.Screen != "" || r.Writable || len(r.Fields) != 0 {
		t.Fatalf("retirement entry advertises private data or CRUD: %+v", r)
	}
	if len(r.Commands) != 1 || r.Commands[0].Verb != "revoke-self" || !r.Commands[0].Collection {
		t.Fatalf("retirement commands = %+v, want one collection command", r.Commands)
	}
	path := r.Commands[0].Path
	if path == "" {
		path = r.Path + "/" + r.Commands[0].Verb
	}
	if res := call(t, router, http.MethodPost, path, "", withSession(session)); res.Code != http.StatusForbidden {
		t.Fatalf("session at advertised retirement = %d %s, want 403", res.Code, res.Body.String())
	}
	if res := call(t, router, http.MethodPost, path, "", bearer(key)); res.Code != http.StatusNoContent || res.Body.Len() != 0 {
		t.Fatalf("key at advertised retirement = %d %s, want empty 204", res.Code, res.Body.String())
	}
	if res := call(t, router, http.MethodPost, path, "", bearer(key)); res.Code != http.StatusUnauthorized {
		t.Errorf("retired key repeating advertised command = %d %s, want 401", res.Code, res.Body.String())
	}
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var count int64
		if err := tx.DB().Table("platformkit_outbox").Where("name = ?", contracts.EventAPITokenRevoked).Count(&count).Error; err != nil {
			return err
		}
		if count != 1 {
			t.Errorf("retirement and refused replay emitted %d events, want one", count)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
