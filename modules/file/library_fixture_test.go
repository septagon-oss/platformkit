package file_test

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/file"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
	"github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/page"
)

const libraryAddress = "/app/file/files"

func libraryFixture(t *testing.T) (chi.Router, *sql.DB, *db.Conn, contracts.Service, module.Module) {
	t.Helper()
	router, admin, conn, svc, manifest, _ := libraryFixtureAPI(t)
	return router, admin, conn, svc, manifest
}

func libraryFixtureAPI(t *testing.T) (chi.Router, *sql.DB, *db.Conn, contracts.Service, module.Module, *httpx.API) {
	t.Helper()
	admin, conn := dbtest.Schema(t, file.Migrations)
	resolved := caller{hosts: map[string]tenancy.Tenant{host: acme, otherHost: globex}}
	api, router := httpx.New(httpx.Options{
		SystemAccess: declaredCrossTenantAccess(),
		Cache:        cache.Memory("pkit"), PublicHost: host, Tenants: resolved, Conn: conn, Authorize: resolved,
		Authenticate: func(_ context.Context, _ db.Tx[db.Tenant], r *http.Request) (tenancy.Principal, bool, error) {
			principal := tenancy.Principal{UserID: uuid.New()}
			if cookie, err := r.Cookie(httpx.CookieName(httpx.SessionCookie, false)); err == nil && cookie.Value == "read-only" {
				principal.Permissions = []string{contracts.PermissionFileRead}
			}
			return principal, r.Header.Get("Cookie") != "", nil
		}, Log: slog.New(slog.DiscardHandler),
	})
	sheet := ui.Compose(design.Default())
	shell := page.Shell{
		Chrome:   page.Chrome{Brand: "PlatformKit", Assets: "/assets", Stylesheet: sheet, SignIn: "/app/admin/login"},
		Frame:    func(_ context.Context, _ page.Request, body []g.Node) g.Node { return page.Bare(body) },
		Tag:      "file",
		Messages: xtext.Load("en", page.Catalogue(), file.Catalogue()),
	}
	svc, manifest := file.New(file.Deps{
		Storage: file.Local(t.TempDir()), MaxBytes: 16 << 20,
		Tenants: widthsOnly{walking: []tenancy.Tenant{acme}}, VariantEvery: time.Second,
		Library: &file.LibraryChrome{Shell: shell, Authorize: resolved},
	})
	manifest.Routes(surfacesOf(api))
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatal(err)
	}
	router.Get("/assets/app.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		_, _ = w.Write(sheet.Body)
	})
	return router, admin, conn, svc, manifest, api
}

func libraryRequest(t *testing.T, router http.Handler, method, at, tenantHost, body string, signed bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "http://"+tenantHost+at, strings.NewReader(body))
	if strings.HasPrefix(at, "/app/") {
		req.Header.Set("Accept", "text/html")
	}
	if signed {
		req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
	}
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}
