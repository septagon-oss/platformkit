package internal_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/user"
	"github.com/septagon-oss/platformkit/ui/screens"
)

func TestTheCatalogCarriesTheTokenSelfRevocationCommand(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	router, _, _, api := mountRecorded(t, conn, auth.OIDC{}, false)
	app.WorkspaceCatalogRoute(func(ctx context.Context, resources []httpx.Resource) (*screens.Catalog, error) {
		doc := screens.Describe(ctx, resources)
		return &doc, nil
	})(api)
	person(t, conn, "ada@acme.localhost", contracts.RoleAdmin)
	session := signIn(t, router, "ada@acme.localhost")
	key, _ := mintKey(t, router, session, "Phone")
	res := call(t, router, http.MethodGet, "/api/v1/app/resources", "", bearer(key))
	if res.Code != http.StatusOK {
		t.Fatalf("catalog = %d %s, want 200", res.Code, res.Body.String())
	}
	var doc screens.Catalog
	if err := json.Unmarshal(res.Body.Bytes(), &doc); err != nil {
		t.Fatalf("read catalog: %v", err)
	}
	for _, resource := range doc.Resources {
		for _, command := range resource.Commands {
			if resource.Module == "auth" && resource.Entity == "token" && command.Verb == "revoke-self" {
				path := command.Path
				if path == "" && command.Collection {
					path = resource.Path + "/" + command.Verb
				}
				if path == revokeSelf {
					return
				}
				t.Errorf("catalog self-revocation endpoint = %q, want %q without a row id", path, revokeSelf)
			}
		}
	}
	t.Errorf("catalog carries no auth-token-revoke-self command: %s", res.Body.String())
}
