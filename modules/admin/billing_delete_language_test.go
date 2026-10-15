package admin_test

import (
	"context"
	"html"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

func TestSubscribedPlanDeletionRefusesInTheRequestsLanguage(t *testing.T) {
	owner, conn := dbtest.Schema(t, billing.Migrations)
	_, plans := billing.Module(billing.Deps{Payments: billing.Manual()})
	api, router := httpx.New(httpx.Options{
		PublicHost: host, Installation: operatorHost,
		Tenants: caller{}, Conn: conn, Authorize: caller{},
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: operator.ID, Roles: []string{"admin"}}, true, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	shell := admin.Module(admin.Deps{
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
	path := created.Header().Get("Location")
	if created.Code != http.StatusSeeOther || path == "" {
		t.Fatalf("create plan = %d, location %q", created.Code, path)
	}
	var planID string
	if err := owner.QueryRowContext(t.Context(), `SELECT id FROM billing_plans WHERE code = 'language-plan'`).Scan(&planID); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "http://"+operatorHost+"/api/v1/billing/subscription/subscribe", strings.NewReader(`{"planId":"`+planID+`"}`))
	req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code < 200 || w.Code >= 300 {
		t.Fatalf("subscribe = %d: %s", w.Code, w.Body.String())
	}
	snapshot := func() (string, string, int) {
		t.Helper()
		var plans, subscriptions string
		var events int
		if err := owner.QueryRowContext(t.Context(), `SELECT
			(SELECT json_agg(p ORDER BY p.id)::text FROM billing_plans p),
			(SELECT json_agg(s ORDER BY s.id)::text FROM billing_subscriptions s),
			(SELECT count(*) FROM platformkit_outbox)`).Scan(&plans, &subscriptions, &events); err != nil {
			t.Fatal(err)
		}
		return plans, subscriptions, events
	}
	beforePlans, beforeSubscriptions, beforeEvents := snapshot()
	refused := post(path+"/delete", "")
	if refused.Code != http.StatusConflict || !strings.HasPrefix(refused.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("delete subscribed plan = %d (%s), want HTML 409", refused.Code, refused.Header().Get("Content-Type"))
	}
	body := html.UnescapeString(refused.Body.String())
	if refused.Header().Get("Content-Language") != "pt-PT" || !strings.Contains(body, `lang="pt-PT"`) {
		t.Error("a Portuguese deletion refusal must declare Portuguese")
	}
	if strings.Contains(body, "is still being billed") || strings.Contains(body, "deactivate it instead") {
		t.Error("Portuguese deletion refusal retains English reason: plan language-plan is still being billed; deactivate it instead")
	}
	afterPlans, afterSubscriptions, afterEvents := snapshot()
	if afterPlans != beforePlans || afterSubscriptions != beforeSubscriptions || afterEvents != beforeEvents {
		t.Errorf("refused deletion changed plans=%t, subscriptions=%t, events=%d (before %d)",
			afterPlans != beforePlans, afterSubscriptions != beforeSubscriptions, afterEvents, beforeEvents)
	}
}
