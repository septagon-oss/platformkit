package rest_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/ui/page"
	"github.com/septagon-oss/platformkit/ui/screens"
)

func TestAbsentWriteScreensReturnNotFoundWithoutReadingARow(t *testing.T) {
	for _, grant := range []bool{false, true} {
		for _, shape := range []string{"read verbs", "nil closures", "no write guard", "singleton"} {
			t.Run(shape+map[bool]string{false: "/reader", true: "/writer"}[grant], func(t *testing.T) {
				s := spec
				if shape == "read verbs" {
					s.Operations = []httpx.CRUD{httpx.CRUDList, httpx.CRUDRead}
				}
				api, router, _ := mountAs(t, s, member{"task:read": true, "task:write": grant})
				r := api.Resources()[0]
				reads, writes := 0, 0
				r.Get = func(context.Context, uuid.UUID) (map[string]any, error) {
					reads++
					return map[string]any{"title": "The singleton"}, nil
				}
				if shape != "read verbs" {
					r.Create, r.Update, r.Delete = nil, nil, nil
				}
				if shape == "no write guard" {
					r.Write, r.WriteBy = "", httpx.Auth{}
					r.Create = func(context.Context, map[string]any) (map[string]any, error) { writes++; return nil, nil }
				}
				r.Singleton = shape == "singleton"
				screens.Mount(api.Surfaces(r.Module).App, page.Shell{Frame: func(_ context.Context, _ page.Request, body []g.Node) g.Node { return g.Group(body) }}, screens.Options{}, r)
				paths := []string{"/app/tasks/task/new", "/app/tasks/task/" + uuid.NewString() + "/edit"}
				if r.Singleton {
					paths = []string{"/app/tasks/task/edit", "/app/tasks/task/new"}
				}
				for _, path := range paths {
					if code, body := call(t, router, http.MethodGet, path, ""); code != http.StatusNotFound {
						t.Errorf("GET %s = %d, want 404: %s", path, code, body)
					}
				}
				if reads != 0 || writes != 0 {
					t.Fatalf("absent screen called Get %d times and wrote %d times", reads, writes)
				}
				if r.Singleton {
					code, body := call(t, router, http.MethodGet, "/app/tasks/task", "")
					if code != 200 || reads != 1 || !strings.Contains(body, "The singleton") || strings.Contains(body, "/edit") {
						t.Fatalf("read-only singleton: %d reads=%d %s", code, reads, body)
					}
				}
				if !r.Singleton {
					code, body := call(t, router, http.MethodGet, "/app/tasks/task", "")
					if code != 200 || strings.Contains(body, "/new") {
						t.Fatalf("read-only list offers a missing form: %d %s", code, body)
					}
				}
			})
		}
	}
}

func TestWriteScreenAvailabilityIsPerVerbAndAuthorityStaysPerRequest(t *testing.T) {
	for _, verb := range []httpx.CRUD{httpx.CRUDCreate, httpx.CRUDUpdate} {
		for _, grant := range []bool{false, true} {
			t.Run(string(verb)+map[bool]string{false: "/denied", true: "/allowed"}[grant], func(t *testing.T) {
				api, router, admin := mountAs(t, spec, member{"task:read": true, "task:write": grant})
				row := seed(t, admin, "An available form")
				r := api.Resources()[0]
				if verb == httpx.CRUDCreate {
					r.Update = nil
				} else {
					r.Create = nil
				}
				r.Delete = nil
				screens.Mount(api.Surfaces(r.Module).App, page.Shell{Frame: func(_ context.Context, _ page.Request, body []g.Node) g.Node { return g.Group(body) }}, screens.Options{}, r)
				newPath, editPath := "/app/tasks/task/new", "/app/tasks/task/"+row+"/edit"
				present, absent := newPath, editPath
				if verb == httpx.CRUDUpdate {
					present, absent = editPath, newPath
				}
				want := http.StatusForbidden
				if grant {
					want = http.StatusOK
				}
				if code, body := call(t, router, http.MethodGet, present, ""); code != want {
					t.Fatalf("available %s = %d, want %d: %s", present, code, want, body)
				}
				if code, body := call(t, router, http.MethodGet, absent, ""); code != http.StatusNotFound {
					t.Fatalf("absent %s = %d, want 404: %s", absent, code, body)
				}
				_, list := call(t, router, http.MethodGet, "/app/tasks/task", "")
				if strings.Contains(list, newPath) != (grant && verb == httpx.CRUDCreate) {
					t.Fatalf("New affordance does not match its route: %s", list)
				}
			})
		}
	}
}

type screenAuthority struct{ writer uuid.UUID }

func (a screenAuthority) Allowed(ctx context.Context, _ tenancy.Tenant, grant tenancy.Grant) (bool, error) {
	p, _ := tenancy.PrincipalFrom(ctx)
	return grant.Permission == "task:read" || p.UserID == a.writer, nil
}

func TestConcurrentReadersAndWritersUseTheirOwnScreenGrants(t *testing.T) {
	_, conn := dbtest.Schema(t)
	writer, reader := uuid.New(), uuid.New()
	api, router := httpx.New(httpx.Options{
		Cache: cache.Memory("pkit"), PublicHost: host, Tenants: caller{}, Conn: conn,
		Authorize: screenAuthority{writer: writer}, Log: slog.New(slog.DiscardHandler),
		Authenticate: func(_ context.Context, _ db.Tx[db.Tenant], r *http.Request) (tenancy.Principal, bool, error) {
			cookie, _ := r.Cookie(httpx.SessionCookie)
			id, err := uuid.Parse(cookie.Value)
			return tenancy.Principal{UserID: id}, err == nil, err
		},
	})
	spec.Mount(api.Surfaces(spec.Module))
	r := api.Resources()[0]
	screens.Mount(api.Surfaces(r.Module).App, page.Shell{Frame: func(_ context.Context, _ page.Request, body []g.Node) g.Node { return g.Group(body) }}, screens.Options{}, r)
	var calls sync.WaitGroup
	for range 8 {
		for _, actor := range []uuid.UUID{reader, writer} {
			calls.Go(func() {
				req := httptest.NewRequest(http.MethodGet, "http://"+host+"/app/tasks/task/new", nil)
				req.AddCookie(&http.Cookie{Name: httpx.SessionCookie, Value: actor.String()})
				response := httptest.NewRecorder()
				router.ServeHTTP(response, req)
				want := http.StatusForbidden
				if actor == writer {
					want = http.StatusOK
				}
				if response.Code != want {
					t.Errorf("actor %s: %d, want %d", actor, response.Code, want)
				}
			})
		}
	}
	calls.Wait()
}
