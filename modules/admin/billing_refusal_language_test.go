package admin_test

import (
	"context"
	"html"
	"log/slog"
	"net/http"
	"net/http/httptest"
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
	"github.com/septagon-oss/platformkit/modules/billing"
	"github.com/septagon-oss/platformkit/ui/page"
	"github.com/septagon-oss/platformkit/ui/resource"
)

func TestBillingPlanRefusalsSpeakPortugueseAndPreserveRows(t *testing.T) {
	owner, conn := dbtest.Schema(t, billing.Migrations)
	_, plans := billing.New(billing.Deps{Payments: billing.Manual()})
	api, router := httpx.New(httpx.Options{
		Cache:      cache.Memory("pkit"),
		PublicHost: host, Installation: operatorHost,
		Tenants: caller{}, Conn: conn, Authorize: caller{},
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: uuid.New(), Roles: []string{"admin"}}, true, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	shell := admin.New(admin.Deps{
		Modules: []module.Module{plans}, Authorize: caller{}, SignIn: "/api/v1/auth/login",
		Messages: xtext.Load("en", admin.Catalogue(), billing.Catalogue(), page.Catalogue(),
			xtext.Source{Name: "resource", FS: resource.Catalogues()}),
	})
	var grants []tenancy.Grant
	for _, m := range []module.Module{plans, shell} {
		for _, permission := range m.Permissions {
			grants = append(grants, tenancy.Grant{Permission: permission.Key, Operator: permission.Operator})
		}
	}
	api.Declare(grants)
	for _, m := range []module.Module{plans, shell} {
		m.Routes(api.Surfaces(m.Name))
	}
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatal(err)
	}
	post := func(path, form string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "http://"+operatorHost+path, strings.NewReader(form))
		req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Accept", "text/html")
		req.Header.Set("Accept-Language", "pt-PT")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	created := post("/app/billing/plans", "code=language-plan&name=Language+plan&currency=EUR&priceCents=2900&interval=month&active=true")
	if created.Code != http.StatusSeeOther || created.Header().Get("Location") == "" {
		t.Fatalf("create valid plan = %d, location %q", created.Code, created.Header().Get("Location"))
	}
	snapshot := func() (string, int) {
		t.Helper()
		var rows string
		var events int
		if err := owner.QueryRowContext(t.Context(), `SELECT COALESCE(json_agg(p ORDER BY p.id)::text, '[]'), (SELECT count(*) FROM platformkit_outbox) FROM billing_plans p`).Scan(&rows, &events); err != nil {
			t.Fatal(err)
		}
		return rows, events
	}
	for _, tc := range []struct{ name, form, english string }{
		{"currency", "currency=AAA", `currency "AAA" is not an ISO 4217 code`},
		{"code", "code=x", `is not a plan code; a code is 2 to`},
		{"price", "priceCents=100000001", `a price of 100000001 is past the 100000000 this module will bill`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before, eventsBefore := snapshot()
			refused := post(created.Header().Get("Location"), tc.form)
			if refused.Code != http.StatusUnprocessableEntity || !strings.HasPrefix(refused.Header().Get("Content-Type"), "text/html") {
				t.Fatalf("invalid plan = %d (%s), want HTML 422", refused.Code, refused.Header().Get("Content-Type"))
			}
			body := html.UnescapeString(refused.Body.String())
			if refused.Header().Get("Content-Language") != "pt-PT" || !strings.Contains(body, `lang="pt-PT"`) {
				t.Error("a Portuguese plan refusal must declare Portuguese")
			}
			if strings.Contains(body, tc.english) {
				t.Errorf("Portuguese plan refusal retains English reason: %s", tc.english)
			}
			after, eventsAfter := snapshot()
			if after != before || eventsAfter != eventsBefore {
				t.Errorf("refusal changed plans=%t or outbox=%d (before %d)", after != before, eventsAfter, eventsBefore)
			}
		})
	}
}
