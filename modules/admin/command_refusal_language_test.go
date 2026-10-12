package admin_test

import (
	"context"
	"database/sql"
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
	"github.com/septagon-oss/platformkit/modules/task"
	"github.com/septagon-oss/platformkit/ui/page"
	"github.com/septagon-oss/platformkit/ui/resource"
)

func taskLanguageRouter(t *testing.T) (*sql.DB, http.Handler) {
	t.Helper()
	owner, conn := dbtest.Schema(t, task.Migrations)
	api, router := httpx.New(httpx.Options{
		PublicHost: host, Tenants: caller{}, Conn: conn, Authorize: caller{},
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: uuid.New(), Roles: []string{"admin"}}, true, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	tasks := task.Module(task.Deps{})
	shell := admin.Module(admin.Deps{
		Modules: []module.Module{tasks}, Authorize: caller{}, SignIn: "/api/v1/auth/login",
		Messages: xtext.Load("en", admin.Catalogue(), task.Catalogue(), page.Catalogue(),
			xtext.Source{Name: "resource", FS: resource.Catalogues()}),
	})
	var grants []tenancy.Grant
	for _, m := range []module.Module{tasks, shell} {
		for _, permission := range m.Permissions {
			grants = append(grants, tenancy.Grant{Permission: permission.Key, Operator: permission.Operator})
		}
	}
	api.Declare(grants)
	for _, m := range []module.Module{tasks, shell} {
		m.Routes(api.Surfaces(m.Name))
	}
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatal(err)
	}
	return owner, router
}

func postTaskInPortuguese(t *testing.T, router http.Handler, path, form string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "http://"+host+path, strings.NewReader(form))
	req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "text/html")
	req.Header.Set("Accept-Language", "pt-PT")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestClosedTaskCommandRefusalSpeaksTheRequestsLanguage(t *testing.T) {
	owner, router := taskLanguageRouter(t)
	created := postTaskInPortuguese(t, router, "/app/task/tasks", "title=Language+case&status=open&priority=normal")
	if created.Code != http.StatusSeeOther || created.Header().Get("Location") == "" {
		t.Fatalf("create = %d, location %q", created.Code, created.Header().Get("Location"))
	}
	if _, err := owner.ExecContext(t.Context(), `UPDATE tasks SET status = 'closed', resolved_at = now()`); err != nil {
		t.Fatal(err)
	}
	var eventsBefore int
	if err := owner.QueryRowContext(t.Context(), `SELECT count(*) FROM platformkit_outbox`).Scan(&eventsBefore); err != nil {
		t.Fatal(err)
	}
	w := postTaskInPortuguese(t, router, created.Header().Get("Location")+"/resolve", "resolution=Finished")
	if w.Code != http.StatusConflict {
		t.Fatalf("closed task resolve = %d, want 409", w.Code)
	}
	body := w.Body.String()
	if strings.Contains(body, "a closed task cannot be resolved") {
		t.Error("Portuguese command refusal retains English reason: a closed task cannot be resolved")
	}
	if !strings.Contains(body, `lang="pt-PT"`) || w.Header().Get("Content-Language") != "pt-PT" {
		t.Errorf("Portuguese command refusal declares Content-Language=%q and pt-PT document=%t",
			w.Header().Get("Content-Language"), strings.Contains(body, `lang="pt-PT"`))
	}
	var status, resolution string
	var eventsAfter int
	if err := owner.QueryRowContext(t.Context(), `SELECT status, resolution FROM tasks`).Scan(&status, &resolution); err != nil {
		t.Fatal(err)
	}
	if err := owner.QueryRowContext(t.Context(), `SELECT count(*) FROM platformkit_outbox`).Scan(&eventsAfter); err != nil {
		t.Fatal(err)
	}
	if status != "closed" || resolution != "" || eventsAfter != eventsBefore {
		t.Errorf("refusal changed status=%q resolution=%q events=%d (was %d)", status, resolution, eventsAfter, eventsBefore)
	}
}
