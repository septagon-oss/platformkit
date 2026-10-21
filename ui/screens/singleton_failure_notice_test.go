package screens_test

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/ui/page"
	"github.com/septagon-oss/platformkit/ui/screens"
)

func TestASingletonFormWordsItsFailureWithoutTheServersError(t *testing.T) {
	_, conn := dbtest.Schema(t)
	api, router := httpx.New(httpx.Options{
		Cache: cache.Memory("pkit"), PublicHost: formHost, Tenants: formDirectory{}, Authorize: formDirectory{},
		Conn: conn, Log: slog.New(slog.DiscardHandler),
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: uuid.MustParse("22222222-2222-4222-8222-222222222222")}, true, nil
		},
	})
	shell := page.Shell{Frame: func(_ context.Context, _ page.Request, body []g.Node) g.Node {
		return g.Group(body)
	}}
	res := resource()
	res.Singleton = true
	res.Operations = []httpx.CRUD{httpx.CRUDRead, httpx.CRUDUpdate}
	res.Get = func(context.Context, uuid.UUID) (map[string]any, error) {
		return map[string]any{"title": "Saved title"}, nil
	}
	var failure error
	calls := 0
	res.Update = func(_ context.Context, id uuid.UUID, values map[string]any) (map[string]any, error) {
		calls++
		if id != uuid.Nil || values["title"] != "Call the harbour" {
			t.Errorf("singleton update received id=%s values=%v", id, values)
		}
		return nil, failure
	}
	screens.Mount(api.Surfaces("note").App, shell, opts, res)
	if got := submitNote(t, router, "Call the harbour"); got.Code != http.StatusSeeOther || calls != 1 {
		t.Fatalf("successful singleton update: status=%d calls=%d", got.Code, calls)
	}

	const driverError = "pq: could not serialize access (SQLSTATE 40001)"
	for _, tc := range []struct {
		name, want, reject string
		err                error
	}{
		{"unmapped failure", "We couldn&#39;t save this. Nothing was saved.", "SQLSTATE", errors.New(driverError)},
		{"problem failure", "We couldn&#39;t save this. Nothing was saved.", "SQLSTATE", &problem.Problem{Status: 500, Detail: driverError}},
		{"plan exclusion", "This isn&#39;t included in this workspace&#39;s plan.", "note-sharing", &problem.Problem{Status: 402, Detail: httpx.CodePlanExcludes + ": this plan excludes note-sharing"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			failure = tc.err
			before := calls
			got := submitNote(t, router, "Call the harbour")
			body := got.Body.String()
			if calls != before+1 || got.Code < 400 || !strings.Contains(body, `value="Call the harbour"`) {
				t.Fatalf("refused singleton update: status=%d calls=%d; typed value kept=%t", got.Code, calls-before, strings.Contains(body, `value="Call the harbour"`))
			}
			if strings.Contains(body, tc.reject) {
				t.Errorf("singleton failure exposes %q", tc.reject)
			}
			if !strings.Contains(body, tc.want) {
				t.Errorf("singleton failure omits %q", tc.want)
			}
			if tc.name == "plan exclusion" && !strings.Contains(body, `data-alert-tone="info"`) {
				t.Error("singleton plan exclusion is not a quiet info notice")
			}
		})
	}
}
