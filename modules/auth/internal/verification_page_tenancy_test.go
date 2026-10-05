package internal_test

import (
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	notification "github.com/septagon-oss/platformkit/modules/notification"
	usermodule "github.com/septagon-oss/platformkit/modules/user"
)

func TestVerificationPageNeitherReadsNorSpendsAnotherTenantsCredential(t *testing.T) {
	_, conn := dbtest.Schema(t, usermodule.Migrations, notification.Migrations, auth.Migrations)
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, emailSignup)
	const address = "private-recipient@example.com"
	_, token := verificationSignup(t, conn, router, address)
	seed(t, conn, globex)
	deps := auth.Deps{Users: realUsers(), Mailer: mailbox, Hosts: authtest.Host(host), PublicHost: host}
	emailSignup(&deps)
	svc, mod := auth.Module(deps)
	api, tenants := httpx.New(httpx.Options{
		Cache: cache.Memory("pkit"), PublicHost: host, Conn: conn,
		Authorize: svc, Authenticate: svc.Authenticate,
		Tenants: verificationSites{host: acme, "globex.localhost": globex},
		Log:     slog.New(slog.DiscardHandler),
	})
	api.Declare([]tenancy.Grant{{Permission: contracts.PermissionRoleManage}})
	mod.Routes(surfacesOf(api))
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatal(err)
	}
	link := contracts.VerifyEmailPath + "?token=" + url.QueryEscape(token)
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		path, body := link, ""
		if method == http.MethodPost {
			path, body = contracts.VerifyEmailPath, "token="+url.QueryEscape(token)
		}
		res := call(t, tenants, method, path, body, func(r *http.Request) {
			r.Host = "globex.localhost"
			r.Header.Set("Accept", "text/html")
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			r.Header.Set("Sec-Fetch-Site", "same-origin")
		})
		if res.Code != http.StatusUnauthorized || strings.Contains(res.Body.String(), address) {
			t.Errorf("foreign %s exposed the credential's addressee or accepted it: status %d", method, res.Code)
		}
	}
	if res := call(t, tenants, http.MethodGet, link, ""); res.Code != http.StatusOK || !strings.Contains(res.Body.String(), address) {
		t.Fatal("the issuing tenant cannot read its own live verification page")
	}
	if res := call(t, tenants, http.MethodPost, "/api/v1/public/auth/verify-email", verificationBody(t, token)); res.Code != http.StatusOK {
		t.Errorf("foreign page requests consumed the issuing tenant's credential: %d", res.Code)
	}
}
