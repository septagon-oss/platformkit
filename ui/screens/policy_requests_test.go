package screens_test

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/ui/page"
	"github.com/septagon-oss/platformkit/ui/screens"
	"golang.org/x/net/html"
	g "maragu.dev/gomponents"
)

type policyDirectory struct {
	formDirectory
	allow *bool
}

func (d policyDirectory) Allowed(_ context.Context, _ tenancy.Tenant, g tenancy.Grant) (bool, error) {
	return g.Permission != "note:act" || *d.allow, nil
}

func policyServer(t *testing.T, res httpx.Resource, allow *bool) http.Handler {
	t.Helper()
	_, conn := dbtest.Schema(t)
	directory := policyDirectory{allow: allow}
	api, router := httpx.New(httpx.Options{Cache: cache.Memory("pkit"), PublicHost: formHost, Tenants: directory, Authorize: directory, Conn: conn, Log: slog.New(slog.DiscardHandler),
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: uuid.MustParse("22222222-2222-4222-8222-222222222222")}, true, nil
		},
	})
	shell := page.Shell{Chrome: page.Chrome{Brand: "Probe"}, Frame: func(_ context.Context, _ page.Request, body []g.Node) g.Node { return g.Group(body) }}
	for _, command := range res.Commands {
		api.AddCommand(res.Module, res.Entity, command)
	}
	api.RegisterResource(res)
	screens.Mount(api.Surfaces("note").App, shell, opts, api.Resources()[0])
	return router
}

func policyRequest(t *testing.T, h http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, "http://"+formHost+path, nil)
	r.AddCookie(&http.Cookie{Name: httpx.SessionCookie, Value: "present"})
	r.Header.Set("Accept", "text/html")
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "http://"+formHost)
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	out := httptest.NewRecorder()
	h.ServeHTTP(out, r)
	return out
}

func TestMountedListUsesTheResolvedQueryAndCommands(t *testing.T) {
	allowed := true
	res := resource()
	res.Present = entity.EntryHints{Sortable: []string{"title"}}
	calls := 0
	var query crud.Query
	res.List = func(_ context.Context, q crud.Query) ([]map[string]any, int64, error) {
		calls++
		query = q
		return []map[string]any{{"id": "one", "title": "A", "status": "open", "pinned": false}}, 101, nil
	}
	run := func(context.Context, uuid.UUID, map[string]any) error { return nil }
	res.Commands = []httpx.Command{{Verb: "act", Summary: "Raw summary", Present: entity.CommandHints{Label: "Resolved action", Primary: true}, Collection: true, Auth: httpx.Permission("note:act"), Run: run},
		{Verb: "system", Summary: "System action", Present: entity.CommandHints{System: true}, Collection: true, Auth: httpx.SignedIn(), Run: run},
		{Verb: "unwired", Summary: "Unwired", Collection: true, Auth: httpx.SignedIn()}}
	h := policyServer(t, res, &allowed)
	out := policyRequest(t, h, "GET", "/app/note/notes?page=2&sort=-title&filter=status:open&filter=pinned:false")
	if out.Code != 200 {
		t.Fatalf("list=%d %s", out.Code, out.Body.String())
	}
	if query.Sort != "-title" || query.Offset != 50 || !reflect.DeepEqual(query.Filter, map[string]any{"status": "open", "pinned": false}) {
		t.Fatalf("query=%+v", query)
	}
	if body := out.Body.String(); !strings.Contains(body, "filter=status%3Aopen") || !strings.Contains(body, "filter=pinned%3Afalse") {
		t.Error("the page's own sort and pager links lost the selected filters")
	}
	doc := webDOM(t, page.View{Body: []g.Node{g.Raw(out.Body.String())}})
	forms := webFind(doc, func(n *html.Node) bool { return n.Data == "form" && strings.HasSuffix(webAttr(n, "action"), "/act") })
	if len(forms) != 1 || webAttr(forms[0], "aria-label") != "Resolved action" {
		t.Fatal("resolved command missing")
	}
	for _, bad := range []string{"/system", "/unwired", "Raw summary"} {
		if strings.Contains(out.Body.String(), bad) {
			t.Errorf("unexpected %s", bad)
		}
	}
	for _, path := range []string{"?sort=unknown", "?filter=unknown:x", "?filter=status:nope", "?filter=status:open&filter=status:open", "?filter=pinned:False", "?filter=broken"} {
		before := calls
		bad := policyRequest(t, h, "GET", "/app/note/notes"+path)
		if bad.Code != 422 || calls != before {
			t.Fatalf("invalid %s=%d, calls %d→%d", path, bad.Code, before, calls)
		}
	}
	allowed = false
	out = policyRequest(t, h, "GET", "/app/note/notes?success=Retired")
	if out.Code != 200 || strings.Contains(out.Body.String(), "Resolved action") || strings.Contains(out.Body.String(), "Retired") {
		t.Fatal("revoked command or forged success rendered")
	}
	if query.Sort != "-createdAt" {
		t.Errorf("default sort=%q", query.Sort)
	}
}

func TestCommandSuccessCopyAppearsOnlyAfterItsSuccessfulPost(t *testing.T) {
	allowed := true
	refuse := false
	runs := 0
	res := resource()
	res.Commands = []httpx.Command{{Verb: "retire", Summary: "Retire", Present: entity.CommandHints{SuccessMessage: "Retired"}, Collection: true, Auth: httpx.Permission("note:act"), Run: func(context.Context, uuid.UUID, map[string]any) error {
		if refuse {
			return fmt.Errorf("%w: revision changed", crud.ErrConflict)
		}
		runs++
		return nil
	}}}
	h := policyServer(t, res, &allowed)
	out := policyRequest(t, h, "POST", "/app/note/notes/retire")
	if out.Code != 200 || runs != 1 || !strings.Contains(out.Body.String(), "Retired") || !strings.Contains(out.Body.String(), "Continue") {
		t.Fatalf("success=%d %s", out.Code, out.Body.String())
	}
	refuse = true
	out = policyRequest(t, h, "POST", "/app/note/notes/retire")
	if out.Code != 409 || runs != 1 || strings.Contains(out.Body.String(), "Retired") {
		t.Fatalf("refusal=%d %s", out.Code, out.Body.String())
	}
	allowed = false
	out = policyRequest(t, h, "POST", "/app/note/notes/retire")
	if out.Code != 403 || runs != 1 {
		t.Fatalf("revoked POST=%d, writes=%d", out.Code, runs)
	}
}

func TestCommandResultOffersOnlyCallableVisibleUndo(t *testing.T) {
	for _, state := range []string{"callable", "unwired", "system"} {
		t.Run(state, func(t *testing.T) {
			allowed := true
			res := resource()
			undo := httpx.Command{Verb: "undo", Summary: "Raw undo", Collection: true, Auth: httpx.SignedIn(), Present: entity.CommandHints{Label: "Undo the change", Destructive: true}, Run: func(context.Context, uuid.UUID, map[string]any) error { return nil }}
			if state == "unwired" {
				undo.Run = nil
			}
			if state == "system" {
				undo.Present.System = true
			}
			res.Commands = []httpx.Command{{Verb: "cast", Summary: "Cast", Collection: true, Auth: httpx.SignedIn(), HasResult: true, Undo: "undo",
				Output: []entity.Field{{Name: "amount", Type: entity.TypeInt}},
				Run:    func(context.Context, uuid.UUID, map[string]any) error { return nil },
				Execute: func(context.Context, uuid.UUID, map[string]any) (httpx.CommandReply, error) {
					return httpx.CommandReply{Result: map[string]any{"amount": int64(9007199254740993)}, UndoToken: "opaque-credential"}, nil
				},
			}, undo}
			out := policyRequest(t, policyServer(t, res, &allowed), "POST", "/app/note/notes/cast")
			body := out.Body.String()
			if out.Code != 200 || !strings.Contains(body, "9007199254740993") || !strings.Contains(out.Header().Get("Cache-Control"), "no-store") {
				t.Fatalf("result=%d %s", out.Code, body)
			}
			offered := strings.Contains(body, `action="/app/note/notes/undo"`)
			if offered != (state == "callable") {
				t.Fatalf("%s undo offered=%v", state, offered)
			}
			if offered && (!strings.Contains(body, "Undo the change") || !strings.Contains(body, `data-tone="danger"`) || !strings.Contains(body, `value="opaque-credential"`)) {
				t.Fatal("undo lost policy or credential")
			}
			if strings.Contains(body, "Command completed") {
				t.Fatal("empty success copy gained a sentence")
			}
		})
	}
}
