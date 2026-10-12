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
	"github.com/septagon-oss/platformkit/modules/task"
	"github.com/septagon-oss/platformkit/ui/page"
	"github.com/septagon-oss/platformkit/ui/resource"
)

func TestGeneratedTaskValidationSpeaksTheRequestsLanguage(t *testing.T) {
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
	req := httptest.NewRequest(http.MethodPost, "http://"+host+"/app/task/tasks",
		strings.NewReader("title=&status=open&priority=normal"))
	req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "text/html")
	req.Header.Set("Accept-Language", "pt-PT")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	body := w.Body.String()
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(body, `name="title"`) {
		t.Fatalf("task validation = %d, want a 422 form with its title control", w.Code)
	}
	if !strings.Contains(body, `lang="pt-PT"`) {
		t.Error("Portuguese task validation does not declare pt-PT")
	}
	if strings.Contains(body, "a task needs a title") {
		t.Error("Portuguese task validation retains English reason: a task needs a title")
	}
	var rows, events int
	if err := owner.QueryRowContext(t.Context(),
		`SELECT (SELECT count(*) FROM tasks), (SELECT count(*) FROM platformkit_outbox)`).Scan(&rows, &events); err != nil {
		t.Fatal(err)
	}
	if rows != 0 || events != 0 {
		t.Errorf("refused task creation left %d rows and %d events", rows, events)
	}
}
