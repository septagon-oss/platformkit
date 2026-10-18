package admin_test

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/admin"
	"github.com/septagon-oss/platformkit/modules/site"
	"github.com/septagon-oss/platformkit/ui/page"
	"github.com/septagon-oss/platformkit/ui/resource"
)

func TestInvalidHomeSlugRefusesInTheRequestsLanguage(t *testing.T) {
	owner, conn := dbtest.Schema(t, site.Migrations)
	_, settings := site.New(site.Deps{})
	api, router := httpx.New(httpx.Options{
		Cache:      cache.Memory("pkit"),
		PublicHost: host, Tenants: caller{}, Conn: conn, Authorize: caller{},
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: uuid.New(), Roles: []string{"admin"}}, true, nil
		}, Log: slog.New(slog.DiscardHandler),
	})
	shell := admin.New(admin.Deps{Modules: []module.Module{settings}, Authorize: caller{}, SignIn: "/api/v1/auth/login",
		Messages: xtext.Load("en", admin.Catalogue(), site.Catalogue(), page.Catalogue(), xtext.Source{Name: "resource", FS: resource.Catalogues()})})
	api.Declare([]tenancy.Grant{{Permission: "site:manage"}, {Permission: "gallery:read"}})
	settings.Routes(api.Surfaces(settings.Name))
	shell.Routes(api.Surfaces(shell.Name))
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatal(err)
	}
	created := postTaskInPortuguese(t, router, "/app/site/settings", "title=Language+case&homeSlug=welcome")
	if created.Code != http.StatusSeeOther {
		t.Fatalf("save valid settings = %d", created.Code)
	}
	var before, after string
	var eventsBefore, eventsAfter int
	if err := owner.QueryRowContext(t.Context(), `SELECT row_to_json(s)::text, (SELECT count(*) FROM platformkit_outbox) FROM site_settings s`).Scan(&before, &eventsBefore); err != nil {
		t.Fatal(err)
	}
	refused := postTaskInPortuguese(t, router, "/app/site/settings", "title=Changed&homeSlug=invalid%40slug")
	if refused.Code != http.StatusUnprocessableEntity || !strings.HasPrefix(refused.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("invalid slug refusal = %d (%s), want HTML 422", refused.Code, refused.Header().Get("Content-Type"))
	}
	if refused.Header().Get("Content-Language") != "pt-PT" || !strings.Contains(refused.Body.String(), `lang="pt-PT"`) {
		t.Errorf("Portuguese settings refusal declares Content-Language=%q, pt-PT document=%t", refused.Header().Get("Content-Language"), strings.Contains(refused.Body.String(), `lang="pt-PT"`))
	}
	if strings.Contains(refused.Body.String(), "is not a slug, so nothing can be served at it") {
		t.Error("Portuguese settings refusal retains English reason: is not a slug, so nothing can be served at it")
	}
	if err := owner.QueryRowContext(t.Context(), `SELECT row_to_json(s)::text, (SELECT count(*) FROM platformkit_outbox) FROM site_settings s`).Scan(&after, &eventsAfter); err != nil {
		t.Fatal(err)
	}
	if after != before || eventsAfter != eventsBefore {
		t.Errorf("refusal changed settings=%t or outbox=%d (before %d)", after != before, eventsAfter, eventsBefore)
	}
}
