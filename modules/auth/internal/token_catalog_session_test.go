package internal_test

import (
	"context"
	"net/http"
	"strings"
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

// TestASessionsCatalogOffersNoKeyRetirement: the key-only command is absent for a
// session, which the route would refuse, and the token resource it rides on is
// never listed as something to read.
func TestASessionsCatalogOffersNoKeyRetirement(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	router, _, _, api := mountRecorded(t, conn, auth.OIDC{}, false)
	app.WorkspaceCatalogRoute(func(ctx context.Context, resources []httpx.Resource) (*screens.Catalog, error) {
		doc := screens.Describe(ctx, resources)
		return &doc, nil
	})(api)
	person(t, conn, "ada@acme.localhost", contracts.RoleAdmin)
	res := call(t, router, http.MethodGet, "/api/v1/app/resources", "", withSession(signIn(t, router, "ada@acme.localhost")))
	if res.Code != http.StatusOK {
		t.Fatalf("catalog = %d %s, want 200", res.Code, res.Body.String())
	}
	if strings.Contains(res.Body.String(), `"revoke-self"`) || strings.Contains(res.Body.String(), `"entity":"token"`) {
		t.Errorf("a session's catalog names the key-only command or the token resource: %s", res.Body.String())
	}
}
