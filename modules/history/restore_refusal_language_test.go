package history_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/admin"
	"github.com/septagon-oss/platformkit/modules/content"
	contentcontracts "github.com/septagon-oss/platformkit/modules/content/contracts"
	"github.com/septagon-oss/platformkit/modules/history"
	"github.com/septagon-oss/platformkit/modules/translation/contracts/translationtest"
	"github.com/septagon-oss/platformkit/ui/page"
	"github.com/septagon-oss/platformkit/ui/resource"
	"github.com/septagon-oss/platformkit/ui/screens"
)

func contentHistoryFixture(t *testing.T, adminShell ...bool) (*world, http.Handler, contentcontracts.Content, string) {
	t.Helper()
	owner, app := dbtest.Schema(t, history.Migrations, content.Migrations)
	_, versions, _, err := history.New(history.Deps{})
	if err != nil {
		t.Fatal(err)
	}
	// A page's title and body declare themselves translatable, so the mount needs
	// the port that answers `?lang=` as much as it needs the store that keeps its
	// writes: this fixture is about the version series, and translationtest.NewFake
	// is the answer the page store's own test gives.
	_, manifest := content.New(content.Deps{
		Versions:     versions,
		Translations: translationtest.NewFake(nil),
	})
	api, router := httpx.New(httpx.Options{
		Cache: cache.Memory("pkit"), PublicHost: acmeHost, Installation: acmeHost,
		Tenants: hosts{byHost: map[string]tenancy.Tenant{acmeHost: acme}}, Conn: app,
		Authorize: grants{contentcontracts.PermissionContentRead: true, contentcontracts.PermissionContentManage: true},
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: principal}, true, nil
		}, Log: slog.New(slog.DiscardHandler),
	})
	manifest.Routes(api.Surfaces("content"))
	res := api.Resources()[0]
	messages := xtext.Load("en", page.Catalogue(), content.Catalogue(),
		xtext.Source{FS: resource.Catalogues(), Name: "ui/resource"})
	if len(adminShell) > 0 && adminShell[0] {
		admin.New(admin.Deps{Modules: []module.Module{manifest}, Messages: messages,
			Authorize: grants{contentcontracts.PermissionContentRead: true, contentcontracts.PermissionContentManage: true},
			SignIn:    "/api/v1/auth/login", SignInOperationID: "auth-login"}).Routes(api.Surfaces("admin"))
	} else {
		screens.Mount(api.Surfaces("content").App, page.Shell{
			Messages: messages,
			Frame:    func(_ context.Context, _ page.Request, body []g.Node) g.Node { return g.Group(body) },
		}, screens.Options{Workspace: "/app", Home: "Dashboard"}, res)
	}
	const path = "/api/v1/content/contents"
	code, body := call(t, router, acmeHost, http.MethodPost, path,
		`{"slug":"language","title":"First draft","body":"Earlier text"}`)
	if code != http.StatusCreated {
		t.Fatalf("create page = %d %s", code, body)
	}
	var created contentcontracts.Content
	if err := json.Unmarshal([]byte(body), &created); err != nil {
		t.Fatal(err)
	}
	if code, body := call(t, router, acmeHost, http.MethodPatch, path+"/"+created.ID.String(),
		`{"body":"Current text"}`); code != http.StatusOK {
		t.Fatalf("edit page = %d %s", code, body)
	}
	return &world{t: t, admin: owner, app: app, store: versions}, router, created, res.Screen
}

func contentRestoreForm(t *testing.T, router http.Handler, created contentcontracts.Content, screen, form, language string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost,
		"http://"+acmeHost+screen+"/"+created.ID.String()+"/restore", strings.NewReader(form))
	req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "text/html")
	req.Header.Set("Accept-Language", language)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestAContentRestoreRefusalSpeaksTheRequestedLanguage(t *testing.T) {
	_, router, created, screen := contentHistoryFixture(t)
	rec := contentRestoreForm(t, router, created, screen, "version=0&expectedRevision=2", "pt-PT")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid version = %d %s, want 422", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Language") != "pt-PT" || !strings.Contains(rec.Body.String(), `lang="pt-PT"`) {
		t.Errorf("Portuguese restore refusal answered Content-Language=%q and Portuguese markup=%t",
			rec.Header().Get("Content-Language"), strings.Contains(rec.Body.String(), `lang="pt-PT"`))
	}
}
