package admin_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
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
	"github.com/septagon-oss/platformkit/modules/content"
	"github.com/septagon-oss/platformkit/ui/page"
	"github.com/septagon-oss/platformkit/ui/resource"
)

func TestContentValidationSpeaksTheRequestsLanguage(t *testing.T) {
	owner, conn := dbtest.Schema(t, content.Migrations)
	api, router := httpx.New(httpx.Options{
		PublicHost: host, Tenants: caller{}, Conn: conn, Authorize: caller{},
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: uuid.New(), Roles: []string{"admin"}}, true, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	_, contents := content.Module(content.Deps{})
	shell := admin.Module(admin.Deps{
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
	for _, tc := range []struct{ name, form, english string }{
		{"missing title", "slug=language-case&title=&kind=page", "a page needs a title"},
		{"missing slug", "slug=&title=Language+case&kind=page", "a page needs a slug"},
		{"long title", "slug=language-case&title=" + strings.Repeat("x", 201) + "&kind=page", "a title is at most"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, language := range []string{"en", "pt-PT"} {
				req := httptest.NewRequestWithContext(t.Context(), http.MethodPost,
					"http://"+host+"/app/content/contents", strings.NewReader(tc.form))
				req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				req.Header.Set("Accept", "text/html")
				req.Header.Set("Accept-Language", language)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)
				body := w.Body.String()
				if w.Code != http.StatusUnprocessableEntity || !strings.Contains(body, `name="title"`) {
					t.Fatalf("%s validation = %d, want a 422 content form", language, w.Code)
				}
				if !strings.Contains(body, `lang="`+language+`"`) || w.Header().Get("Content-Language") != language {
					t.Errorf("%s refusal declares Content-Language=%q", language, w.Header().Get("Content-Language"))
				}
				if language == "pt-PT" && strings.Contains(body, tc.english) {
					t.Errorf("Portuguese content validation retains English reason %q", tc.english)
				}
				var rows, events int
				if err := owner.QueryRowContext(t.Context(),
					`SELECT (SELECT count(*) FROM contents), (SELECT count(*) FROM platformkit_outbox)`).Scan(&rows, &events); err != nil {
					t.Fatal(err)
				}
				if rows != 0 || events != 0 {
					t.Errorf("refused content creation left %d rows and %d events", rows, events)
				}
			}
		})
	}
}
