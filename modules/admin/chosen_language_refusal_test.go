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
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/admin"
	"github.com/septagon-oss/platformkit/ui/page"
	g "maragu.dev/gomponents"
)

func TestSignedInGrantRefusalKeepsThePersonsChosenLanguage(t *testing.T) {
	_, conn := dbtest.Schema(t)
	shell := page.Shell{
		Messages: xtext.Load("en", admin.Catalogue(), page.Catalogue()),
		Frame:    func(_ context.Context, _ page.Request, body []g.Node) g.Node { return g.Group(body) },
		Locale: func(_ context.Context, r page.Request) string {
			if r.SignedIn {
				return "pt-PT"
			}
			return ""
		},
	}
	api, router := httpx.New(httpx.Options{
		PublicHost: host, Tenants: caller{}, Conn: conn, Authorize: caller{},
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: uuid.New(), Roles: []string{"member"}}, true, nil
		},
		Fault: page.FaultHandler(shell), Log: slog.New(slog.DiscardHandler),
	})
	api.Declare([]tenancy.Grant{{Permission: "secret:read"}})
	routes := api.Surfaces("note").App
	page.Serve(routes, shell, page.Route{ID: "note-language", Method: http.MethodGet, Path: "/language"}, httpx.SignedIn(), func(context.Context, page.Request, *struct{}) (page.View, error) {
		return page.View{Title: "Language"}, nil
	})
	page.Serve(routes, shell, page.Route{ID: "note-private", Method: http.MethodGet, Path: "/private"}, httpx.Permission("secret:read"), func(context.Context, page.Request, *struct{}) (page.View, error) {
		t.Error("a refused route ran its handler")
		return page.View{}, nil
	})
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path   string
		status int
	}{
		{"/app/note/language", http.StatusOK},
		{"/app/note/private", http.StatusForbidden},
	} {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+host+tc.path, nil)
		req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
		req.Header.Set("Accept", "text/html")
		req.Header.Set("Accept-Language", "en")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != tc.status || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
			t.Fatalf("%s = %d (%s), want an HTML %d", tc.path, w.Code, w.Header().Get("Content-Type"), tc.status)
		}
		if w.Header().Get("Content-Language") != "pt-PT" || !strings.Contains(w.Body.String(), `lang="pt-PT"`) {
			t.Errorf("%s ignores the signed-in person's pt-PT choice: Content-Language=%q, pt-PT document=%t",
				tc.path, w.Header().Get("Content-Language"), strings.Contains(w.Body.String(), `lang="pt-PT"`))
		}
	}
}
