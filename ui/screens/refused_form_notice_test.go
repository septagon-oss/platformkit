package screens_test

// A generated form refused on submit, served: the kernel's router and guards, the screens
// this package mounts, and a resource whose write answers with the refusal under test.

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/page"
	"github.com/septagon-oss/platformkit/ui/screens"
)

const formHost = "demo.localhost"

type formDirectory struct{}

func (formDirectory) ByHost(context.Context, db.Tx[db.System], string) (tenancy.Tenant, error) {
	return tenancy.Tenant{ID: uuid.MustParse("11111111-1111-4111-8111-111111111111"), Slug: "acme", Name: "Acme"}, nil
}

func (formDirectory) Allowed(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) {
	return true, nil
}

// formServer mounts the generated note screens; refuse is what the next create answers.
func formServer(t *testing.T, refuse *error) http.Handler {
	t.Helper()
	res := resource()
	res.Operations = []httpx.CRUD{httpx.CRUDList, httpx.CRUDCreate}
	res.List = func(context.Context, crud.Query) ([]map[string]any, int64, error) { return nil, 0, nil }
	res.Create = func(context.Context, map[string]any) (map[string]any, error) {
		if *refuse != nil {
			return nil, *refuse
		}
		return map[string]any{"id": uuid.NewString()}, nil
	}
	return formResourceServer(t, res)
}

func formResourceServer(t *testing.T, res httpx.Resource) http.Handler {
	t.Helper()
	_, conn := dbtest.Schema(t)
	api, router := httpx.New(httpx.Options{
		Cache: cache.Memory("pkit"), PublicHost: formHost, Tenants: formDirectory{}, Authorize: formDirectory{},
		Conn: conn, Log: slog.New(slog.DiscardHandler),
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: uuid.MustParse("22222222-2222-4222-8222-222222222222")}, true, nil
		},
	})
	shell := formShell()
	screens.Mount(api.Surfaces("note").App, shell, opts, res)
	return router
}

func submitNote(t *testing.T, h http.Handler, title string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "http://"+formHost+"/app/note/notes",
		strings.NewReader(url.Values{"title": {title}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Accept", "text/html")
	r.Header.Set("Origin", "http://"+formHost)
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.AddCookie(&http.Cookie{Name: httpx.SessionCookie, Value: "present"})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// TestAFormRefusedByThePlanIsAQuietNoticeInWords: a write the plan excludes is an info
// notice in words, with no code and no feature slug, and the typed value stays in its control.
func TestAFormRefusedByThePlanIsAQuietNoticeInWords(t *testing.T) {
	var refuse error
	h := formServer(t, &refuse)
	if got := submitNote(t, h, "Call the harbour"); got.Code != http.StatusSeeOther {
		t.Fatalf("a create that succeeds = %d; this case is about the one that is refused: %s", got.Code, got.Body.String())
	}

	refuse = &problem.Problem{Status: http.StatusPaymentRequired,
		Detail: httpx.CodePlanExcludes + ": this tenant's plan does not include note-sharing"}
	body := submitNote(t, h, "Call the harbour").Body.String()
	for _, want := range []string{"This isn&#39;t included in this workspace&#39;s plan.", `data-alert-tone="info"`, `value="Call the harbour"`} {
		if !strings.Contains(body, want) {
			t.Errorf("a plan-refused form omits %q", want)
		}
	}
	for _, reject := range []string{"note-sharing", httpx.CodePlanExcludes + ":", `data-alert-tone="danger"`} {
		if strings.Contains(body, reject) {
			t.Errorf("a plan-refused form says %q", reject)
		}
	}
}

func formShell() page.Shell {
	return page.Shell{
		Chrome: page.Chrome{Brand: "Demo", SignIn: "/admin/login", Stylesheet: ui.Compose(design.Default()), Assets: "/admin/assets"},
		Frame: func(_ context.Context, _ page.Request, body []g.Node) g.Node {
			return h.Main(h.ID("main-content"), g.Group(body))
		},
		Back: "/admin", BackLabel: "Back to the workspace",
	}
}
