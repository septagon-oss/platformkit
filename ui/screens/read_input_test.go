package screens_test

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/ui/screens"
)

type readQuestions struct {
	Title  string `json:"title" validate:"required"`
	Rank   int64  `json:"rank" default:"7" ui:"section:details"`
	Pinned bool   `json:"pinned" ui:"section:options"`
}

func readStepServer(t *testing.T, run func(context.Context, db.Tx[db.Tenant], uuid.UUID, readQuestions) (readQuestions, error)) (http.Handler, *sql.DB, httpx.Resource) {
	t.Helper()
	admin, conn := dbtest.Schema(t)
	api, router := httpx.New(httpx.Options{Cache: cache.Memory("pkit"), PublicHost: formHost, Tenants: formDirectory{}, Authorize: formDirectory{}, Conn: conn, Log: slog.New(slog.DiscardHandler),
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: uuid.MustParse("22222222-2222-4222-8222-222222222222")}, true, nil
		}})
	spec := rest.ReadForm(run)
	spec.Module, spec.Entity, spec.Path, spec.ReadAuth = "query", "questions", "/questions", httpx.SignedIn()
	fixture := steppedNote()
	spec.Present = fixture.Present
	spec.Present.Forms = &entity.FormsHints{Input: fixture.Present.Forms.Create}
	spec.Mount(api.Surfaces("query"))
	r := api.Resources()[0]
	screens.Mount(api.Surfaces("query").App, formShell(), opts, r)
	return router, admin, r
}

func TestTypedReadStepsInvokeOnlyOnFinalSubmitAndRenderExactOutput(t *testing.T) {
	calls := 0
	var receivedRank int64
	router, admin, r := readStepServer(t, func(_ context.Context, tx db.Tx[db.Tenant], actor uuid.UUID, in readQuestions) (readQuestions, error) {
		calls++
		receivedRank = in.Rank
		if db.TenantOf(tx).ID == uuid.Nil || actor == uuid.Nil {
			t.Fatal("missing query scope")
		}
		return in, nil
	})
	at := r.Screen
	s := continuation(t, stepRequest(router, at, nil), 200, 0)
	s = continuation(t, postStep(router, at+"/steps", s, "next", url.Values{"title": {"Answer"}}), 200, 1)
	s = continuation(t, postStep(router, at+"/steps", s, "next", url.Values{"rank": {"9007199254740993"}}), 200, 2)
	s = continuation(t, postStep(router, at+"/steps", s, "back", nil), 200, 1)
	if calls != 0 {
		t.Fatalf("navigation queried %d times", calls)
	}
	s = continuation(t, postStep(router, at+"/steps", s, "next", url.Values{"rank": {"9007199254740993"}}), 200, 2)
	out := postStep(router, at, s, "submit", nil)
	if out.Code != 200 || calls != 1 || !strings.Contains(out.Body.String(), "9007199254740993") || !strings.Contains(out.Body.String(), "Answer") {
		t.Fatalf("query result: calls=%d status=%d %s", calls, out.Code, out.Body)
	}
	s = continuation(t, stepRequest(router, at, nil), 200, 0)
	if s.Answers["rank"] != "7" {
		t.Fatalf("initial default: %+v", s.Answers)
	}
	s = continuation(t, postStep(router, at+"/steps", s, "next", url.Values{"title": {"Cleared"}}), 200, 1)
	s = continuation(t, postStep(router, at+"/steps", s, "next", url.Values{"rank": {""}}), 200, 2)
	out = postStep(router, at, s, "submit", nil)
	if out.Code != 200 || calls != 2 || receivedRank != 0 {
		t.Fatalf("blank restored default: status=%d calls=%d rank=%d", out.Code, calls, receivedRank)
	}
	var events int
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM platformkit_outbox").Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != 0 {
		t.Fatalf("read emitted %d events", events)
	}
	if out := stepRequest(router, at+"/new", nil); out.Code != 404 {
		t.Fatalf("query exposes create: %d", out.Code)
	}
}
