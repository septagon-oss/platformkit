package screens_test

// The year a guided sheet is sold for is justified by being the same bytes for whoever
// asks. The pages beside it are not: a step document carries one person's session — its
// title, the sets they recorded, the address of their exit — in the `data-guided` attribute
// its countdown script reads. Every guided case here renders that view and reads its markup,
// so the addresses the mount puts it behind, the caller and transaction they resolve, and the
// headers that keep the answer from outliving the person answered, have never been asked.
// This case mounts a guided resource and walks it as its person does: the session address
// resumes to the pair after the recorded set, that pair's page answers to its owner with the
// headers that keep it theirs, its sheet keeps the year, and the same address answered to a
// caller nobody recognises carries none of it.

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
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

// walkServer mounts one resource's guided screens on the workspace surface, with the sheet
// they link served by the tree beside them. It answers for the caller only while `owner`
// holds: the caller nobody recognised is the one this case has to refuse a page to.
func walkServer(t *testing.T, session entity.GuidedSession, owner bool) http.Handler {
	t.Helper()
	_, conn := dbtest.Schema(t)
	api, router := httpx.New(httpx.Options{
		Cache: cache.Memory("pkit"), PublicHost: formHost, Tenants: formDirectory{}, Authorize: formDirectory{},
		Conn: conn, Log: slog.New(slog.DiscardHandler),
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			if !owner {
				return tenancy.Principal{}, false, nil
			}
			return tenancy.Principal{UserID: uuid.MustParse("22222222-2222-4222-8222-222222222222")}, true, nil
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

// walkAsk fetches one address the way a browser at the workspace does: for a document,
// holding this installation's session cookie only when the caller has one.
func walkAsk(t *testing.T, h http.Handler, address string, withSession bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "http://"+formHost+address, nil)
	req.Header.Set("Accept", "text/html")
	if withSession {
		req.AddCookie(&http.Cookie{Name: httpx.SessionCookie, Value: "present"})
	}
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	return res
}

// TestAGuidedWalkAnswersItsPagesOnlyToThePersonWhoOwnsThem mounts a guided resource and
// walks it as its person does: the session address resumes to the pair after the recorded
// set, that pair's page answers 200 to the owner with a session in progress on it and the
// headers that keep the answer to them, and the sheet its head links is kept for a year. The
// same address, asked by a caller the installation does not recognise, is answered the
// sign-in address with nothing of the session in it. A mount that served a person's own page
// as public, one that answered it to whoever asked, a resume that landed on the first step
// rather than the next pair, and a session page that let a cache hold its body are refused.
func TestAGuidedWalkAnswersItsPagesOnlyToThePersonWhoOwnsThem(t *testing.T) {
	session := oneSetRecorded()
	h := walkServer(t, session, true)
	at := "/app/note/notes/" + session.ID.String()

	// One set is recorded, the first step's only one, so the person owes the first of the second.
	resumed := walkAsk(t, h, at, true)
	want := at + "/steps/" + session.Steps[1].ID.String() + "/sets/" + session.Steps[1].Sets[0].String()
	if resumed.Code != http.StatusSeeOther || resumed.Header().Get("Location") != want {
		t.Fatalf("a person who recorded one set comes back to %s and is sent %d to %q, want 303 to %q",
			at, resumed.Code, resumed.Header().Get("Location"), want)
	}

	owned := walkAsk(t, h, want, true)
	if owned.Code != http.StatusOK {
		t.Fatalf("the pair this person owes answers %d; this case is about the page beside that refusal: %s",
			owned.Code, firstWords(owned.Body.String()))
	}
	body := owned.Body.String()
	for _, mark := range []string{"data-guided", "data-guided-timers", session.Title} {
		if !strings.Contains(body, mark) {
			t.Errorf("the page of a session in progress carries no %q, so the countdown script has nothing to run on: %s",
				mark, firstWords(body))
		}
	}
	// The headers that keep this answer from outliving the person standing in front of it.
	if got := owned.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("the page of one person's session says %q about caching, want %q: their own progress is in the body this address serves",
			got, "no-store")
	}
	if got := owned.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Errorf("the page of one person's session says %q about what it reveals of its own address, want %q: a step's picture address is that person's",
			got, "no-referrer")
	}

	// The one answer of this walk that is anybody's to keep: the sheet its head links.
	bought := sheetTheWalkPaysFor.FindStringSubmatch(body)
	if bought == nil {
		t.Fatal("the step document writes no stylesheet address with a ?v=, so nothing here has a year to keep")
	}
	if got := buy(t, h, bought[1]+"?v="+bought[2]).Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Errorf("the sheet this page links says %q about caching, want %q: the walk pays for its stylesheet once",
			got, "public, max-age=31536000, immutable")
	}

	// The same address, answered to a caller the installation does not recognise.
	anonymous := walkAsk(t, h, want, false)
	if anonymous.Code != http.StatusSeeOther || !strings.HasPrefix(anonymous.Header().Get("Location"), "/app/login?") {
		t.Errorf("a caller with no session is answered %d to %q at somebody else's session page, want the sign-in page this shell names",
			anonymous.Code, anonymous.Header().Get("Location"))
	}
	if got := anonymous.Body.String(); got != "" {
		t.Errorf("the sign-in refusal of a session page carries %d bytes: %s", len(got), firstWords(got))
	}
	if got := anonymous.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("the refusal of somebody's session page says %q about caching, want %q: a refusal of a person's own work is nobody's to keep",
			got, "no-store")
	}
}

// firstWords is the start of an answer, so a failure can show what the caller was actually
// given rather than only that it was not what was wanted.
func firstWords(answer string) string {
	if len(answer) > 400 {
		return answer[:400] + "…"
	}
	return answer
}
