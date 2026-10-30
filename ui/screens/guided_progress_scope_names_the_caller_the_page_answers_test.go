package screens_test

// A guided page hands the countdown script one JSON value in `data-guided`, and its first
// field, `scope`, is the address the script stores a running timer under: the script keys
// every sessionStorage entry by it and drops the entries of any other scope it finds. So the
// scope is not a label — it is what keeps one person's simmer out of another person's
// countdown when one browser carries both. No case here had read it: the guided cases render
// the markup or ask one caller for one page.
// This case answers the same pair to two people of one tenant through one mount, reads each
// document's scope the way the script does, and asks that each name the caller it answered to,
// that the two differ, and that neither answer carries a trace of the other caller.

import (
	"context"
	"encoding/json"
	"html"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"
	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/page"
	"github.com/septagon-oss/platformkit/ui/screens"
)

// guidedScopeWritten is the config the countdown script reads, taken out of the attribute it is
// written to, which is where the script itself reads it from.
var guidedScopeWritten = regexp.MustCompile(`data-guided="([^"]*)"`)

// formTenant is the tenant formDirectory resolves for formHost: the first half of a scope this
// case did not choose, alongside the module and entity resource() declares.
const formTenant = "11111111-1111-4111-8111-111111111111"

// scopeServer mounts one guided resource on the workspace surface and answers for whichever
// caller the request names, so one mount answers two people. The tenant is the one this
// package's directory resolves for its host, so the callers differ only in who they are.
func scopeServer(t *testing.T, session entity.GuidedSession) http.Handler {
	t.Helper()
	_, conn := dbtest.Schema(t)
	api, router := httpx.New(httpx.Options{
		Cache: cache.Memory("pkit"), PublicHost: formHost, Tenants: formDirectory{}, Authorize: formDirectory{},
		Conn: conn, Log: slog.New(slog.DiscardHandler),
		Authenticate: func(_ context.Context, _ db.Tx[db.Tenant], req *http.Request) (tenancy.Principal, bool, error) {
			caller, err := uuid.Parse(req.Header.Get("X-Test-Caller"))
			if err != nil {
				return tenancy.Principal{}, false, nil
			}
			return tenancy.Principal{UserID: caller}, true, nil
		},
	})
	sheet := ui.Compose(design.Default())
	app := api.Surfaces("note").App
	app.Static("assets", ui.Assets(sheet))
	shell := page.Shell{
		Chrome: page.Chrome{Brand: "Practice", SignIn: "/app/login", Assets: app.PagePath("assets"), Stylesheet: sheet},
		Frame: func(_ context.Context, _ page.Request, body []g.Node) g.Node {
			return h.Div(h.Class("pkit-shell"), g.Group(body))
		},
		Back: "/app", BackLabel: "Back to the workspace",
	}
	res := resource()
	res.Guided = func(context.Context, uuid.UUID) (entity.GuidedSession, error) { return session, nil }
	res.GuidedCommands = [2]bool{true, true}
	res.Present = entity.EntryHints{Session: true}
	screens.Mount(app, shell, opts, res)
	return router
}

// guidedScopeAnswered asks one caller's pair page and answers the scope the document writes,
// read out of the JSON the countdown script parses, alongside the whole answer for the trace.
func guidedScopeAnswered(t *testing.T, h http.Handler, address string, caller uuid.UUID) (string, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "http://"+formHost+address, nil)
	req.Header.Set("Accept", "text/html")
	req.Header.Set("X-Test-Caller", caller.String())
	req.AddCookie(&http.Cookie{Name: httpx.SessionCookie, Value: "present"})
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("the pair answers %d to the person it is under, so no scope is written to read: %s",
			res.Code, firstWords(res.Body.String()))
	}
	written := guidedScopeWritten.FindStringSubmatch(res.Body.String())
	if written == nil {
		t.Fatal("the page writes no data-guided config, so the script has no scope to store under: " +
			firstWords(res.Body.String()))
	}
	var config struct {
		Scope string `json:"scope"`
	}
	if err := json.Unmarshal([]byte(html.UnescapeString(written[1])), &config); err != nil {
		t.Fatal("the page's data-guided config is not the JSON the script parses: ", err)
	}
	return config.Scope, res.Body.String()
}

// TestAGuidedProgressScopeNamesTheCallerThePageAnswers answers one guided pair to two people of
// one tenant through one mount and reads the scope each document hands its countdown script.
// Each names the tenant, the caller it was answered to and this resource; the two differ; and
// neither answer carries the other caller's id. A scope that dropped the caller would store one
// person's running timer where the next person's page reads it.
func TestAGuidedProgressScopeNamesTheCallerThePageAnswers(t *testing.T) {
	session := oneSetRecorded()
	h := scopeServer(t, session)
	first := uuid.MustParse("33333333-3333-4333-8333-333333333333")
	second := uuid.MustParse("44444444-4444-4444-8444-444444444444")
	pair := "/app/note/notes/" + session.ID.String() + "/steps/" + session.Steps[1].ID.String() +
		"/sets/" + session.Steps[1].Sets[0].String()

	res := resource()
	answers := map[uuid.UUID]string{}
	bodies := map[uuid.UUID]string{}
	for _, caller := range []uuid.UUID{first, second} {
		scope, body := guidedScopeAnswered(t, h, pair, caller)
		answers[caller], bodies[caller] = scope, body
		if want := formTenant + "/" + caller.String() + "/" + res.Module + "/" + res.Entity; scope != want {
			t.Errorf("the page answered to %s stores its countdown under %q, want %q — the tenant, the caller it is "+
				"answered to and this resource, so nothing another person stores is read as this page's: %s",
				caller, scope, want, firstWords(body))
		}
	}
	for _, caller := range []uuid.UUID{first, second} {
		other := second
		if caller == second {
			other = first
		}
		if strings.Contains(bodies[caller], other.String()) {
			t.Errorf("the page answered to %s carries %s, whose own answer at that address is a different one",
				caller, other)
		}
	}
	if answers[first] == answers[second] {
		t.Errorf("the two people are handed one countdown scope %q, so the timer one leaves running is the other's",
			answers[first])
	}
}
