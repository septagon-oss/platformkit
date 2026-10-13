package admin_test

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/admin"
	"github.com/septagon-oss/platformkit/modules/user"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
	"github.com/septagon-oss/platformkit/ui/page"
	"github.com/septagon-oss/platformkit/ui/resource"
)

func userLanguageRouter(t *testing.T) (*sql.DB, http.Handler) {
	t.Helper()
	owner, conn := dbtest.Schema(t, user.Migrations)
	_, users := user.Module(user.Deps{
		Administration: &usercontracts.AdministrationFunc{Ask: func(context.Context, db.Tx[db.Tenant]) ([]string, error) {
			return []string{"admin"}, nil
		}},
		Granting: &usercontracts.GrantingFunc{Ask: func(context.Context, db.Tx[db.Tenant]) (bool, error) {
			return true, nil
		}},
		LocalePolicy: &usercontracts.LocalePolicyFunc{Ask: func(context.Context, db.Tx[db.Tenant]) (tenancy.Languages, error) {
			return tenancy.Languages{Default: "en", Others: []string{"pt-PT"}}, nil
		}},
	})
	api, router := httpx.New(httpx.Options{
		PublicHost: host, Tenants: caller{}, Conn: conn, Authorize: caller{},
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: uuid.New(), Roles: []string{"admin"}}, true, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	shell := admin.Module(admin.Deps{
		Modules: []module.Module{users}, Authorize: caller{}, SignIn: "/api/v1/auth/login",
		Messages: xtext.Load("en", admin.Catalogue(), user.Catalogue(), page.Catalogue(),
			xtext.Source{Name: "resource", FS: resource.Catalogues()}),
	})
	var grants []tenancy.Grant
	for _, m := range []module.Module{users, shell} {
		for _, permission := range m.Permissions {
			grants = append(grants, tenancy.Grant{Permission: permission.Key, Operator: permission.Operator})
		}
	}
	api.Declare(grants)
	for _, m := range []module.Module{users, shell} {
		m.Routes(api.Surfaces(m.Name))
	}
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatal(err)
	}
	return owner, router
}

func TestReservedHandleRefusesInTheRequestsLanguage(t *testing.T) {
	owner, router := userLanguageRouter(t)
	created := postTaskInPortuguese(t, router, "/app/user/users", "email=reader%40language.test&displayName=Reader")
	path := created.Header().Get("Location")
	if created.Code != http.StatusSeeOther || path == "" {
		t.Fatalf("create = %d, location %q", created.Code, path)
	}
	var beforeRow, afterRow string
	var beforeEvents, afterEvents int
	if err := owner.QueryRowContext(t.Context(), `SELECT row_to_json(u)::text, (SELECT count(*) FROM platformkit_outbox) FROM users u`).Scan(&beforeRow, &beforeEvents); err != nil {
		t.Fatal(err)
	}
	refused := postTaskInPortuguese(t, router, path+"/handle", "handle=admin")
	if refused.Code != http.StatusUnprocessableEntity || !strings.HasPrefix(refused.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("reserved handle refusal = %d (%s), want HTML 422", refused.Code, refused.Header().Get("Content-Type"))
	}
	body := refused.Body.String()
	if refused.Header().Get("Content-Language") != "pt-PT" || !strings.Contains(body, `lang="pt-PT"`) {
		t.Errorf("Portuguese handle refusal declares Content-Language=%q, pt-PT document=%t", refused.Header().Get("Content-Language"), strings.Contains(body, `lang="pt-PT"`))
	}
	if strings.Contains(body, "names the platform or a role, not a person") {
		t.Error("Portuguese handle refusal retains English reason: names the platform or a role, not a person")
	}
	if err := owner.QueryRowContext(t.Context(), `SELECT row_to_json(u)::text, (SELECT count(*) FROM platformkit_outbox) FROM users u`).Scan(&afterRow, &afterEvents); err != nil {
		t.Fatal(err)
	}
	if afterRow != beforeRow || afterEvents != beforeEvents {
		t.Errorf("refusal changed stored row=%t or event count=%d (before %d)", afterRow != beforeRow, afterEvents, beforeEvents)
	}
}
