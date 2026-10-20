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
	"github.com/septagon-oss/platformkit/modules/content"
	"github.com/septagon-oss/platformkit/modules/translation/contracts/translationtest"
	"github.com/septagon-oss/platformkit/ui/page"
	"github.com/septagon-oss/platformkit/ui/resource"
)

func TestArchivedContentPublicationRefusesInTheRequestsLanguage(t *testing.T) {
	owner, conn := dbtest.Schema(t, content.Migrations)
	api, router := httpx.New(httpx.Options{
		Cache:      cache.Memory("pkit"),
		PublicHost: host, Tenants: caller{}, Conn: conn, Authorize: caller{},
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: uuid.New(), Roles: []string{"admin"}}, true, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	_, contents := content.New(content.Deps{Translations: translationtest.NewFake(nil), Versions: noVersions{}})
	shell := admin.New(admin.Deps{
		Modules: []module.Module{contents}, Authorize: caller{}, SignIn: "/api/v1/auth/login",
		Messages: xtext.Load("en", admin.Catalogue(), content.Catalogue(), page.Catalogue(),
			xtext.Source{Name: "resource", FS: resource.Catalogues()}),
	})
	var grants []tenancy.Grant
	for _, m := range []module.Module{contents, shell} {
		for _, permission := range m.Permissions {
			grants = append(grants, tenancy.Grant{Permission: permission.Key, Operator: permission.Operator})
		}
	}
	api.Declare(grants)
	for _, m := range []module.Module{contents, shell} {
		m.Routes(api.Surfaces(m.Name))
	}
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatal(err)
	}
	created := postTaskInPortuguese(t, router, "/app/content/contents", "slug=language-case&title=Language+case&kind=page")
	path := created.Header().Get("Location")
	if created.Code != http.StatusSeeOther || path == "" {
		t.Fatalf("create = %d, location %q", created.Code, path)
	}
	archived := postTaskInPortuguese(t, router, path+"/archive", "")
	if archived.Code < 200 || archived.Code >= 400 {
		t.Fatalf("archive = %d, want a successful command", archived.Code)
	}
	var before int
	var beforeRow string
	if err := owner.QueryRowContext(t.Context(), `SELECT count(*) FROM platformkit_outbox`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := owner.QueryRowContext(t.Context(), `SELECT row_to_json(c)::text FROM contents c`).Scan(&beforeRow); err != nil {
		t.Fatal(err)
	}
	refused := postTaskInPortuguese(t, router, path+"/publish", "")
	if refused.Code != http.StatusConflict || !strings.HasPrefix(refused.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("publication refusal = %d (%s), want HTML 409", refused.Code, refused.Header().Get("Content-Type"))
	}
	body := refused.Body.String()
	if refused.Header().Get("Content-Language") != "pt-PT" || !strings.Contains(body, `lang="pt-PT"`) {
		t.Errorf("Portuguese publication refusal declares Content-Language=%q, pt-PT document=%t",
			refused.Header().Get("Content-Language"), strings.Contains(body, `lang="pt-PT"`))
	}
	if strings.Contains(body, "archived content is not published from the archive") {
		t.Error("Portuguese publication refusal retains English reason: archived content is not published from the archive; unpublish it first")
	}
	var status string
	var published bool
	var after int
	var afterRow string
	if err := owner.QueryRowContext(t.Context(), `SELECT status, published_at IS NOT NULL FROM contents`).Scan(&status, &published); err != nil {
		t.Fatal(err)
	}
	if err := owner.QueryRowContext(t.Context(), `SELECT count(*) FROM platformkit_outbox`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if err := owner.QueryRowContext(t.Context(), `SELECT row_to_json(c)::text FROM contents c`).Scan(&afterRow); err != nil {
		t.Fatal(err)
	}
	if afterRow != beforeRow {
		t.Error("the refused publication changed the stored content row")
	}
	if status != "archived" || published || after != before {
		t.Errorf("refusal left status=%q published=%t events=%d (before %d)", status, published, after, before)
	}
}
