package page_test

// A page that declared its subject is refused about that subject when it is served: the
// kernel's router, its guards and this package's failure page, wired the way an
// application wires them, and a handler whose read failed. Each case first asks the same
// address with a read that succeeds, so a failure below is about the refusal and not about
// reaching the page.

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/ui/page"
)

const (
	subjectList   = "/app/task/tasks"
	subjectRecord = "/app/task/tasks/7"
	// subjectRowTitle is what the record shows when its read succeeds; a refusal of the
	// same record must not carry it.
	subjectRowTitle = "Renew the harbour lease"
)

// subjectServer mounts a generated-style list and record that declare what they are
// about. fail is the error the next read answers with; nil lets the read succeed.
func subjectServer(t *testing.T, fail *error) http.Handler {
	t.Helper()
	_, conn := dbtest.Schema(t)
	shell := shell()
	api, router := httpx.New(httpx.Options{
		Cache: cache.Memory("pkit"), PublicHost: refusalHost, Tenants: refusalDirectory{},
		Authorize: refusalAuthorizer{allow: true}, Conn: conn, Log: slog.New(slog.DiscardHandler),
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: uuid.MustParse("22222222-2222-4222-8222-222222222222")}, true, nil
		},
		Fault: page.FaultHandler(shell),
	})
	tasks := page.Subject{Singular: "task", Plural: "tasks", List: subjectList}
	record := tasks
	record.Record = true
	app := api.Surfaces("task").App
	page.Serve(app, shell, page.Route{ID: "subject-list", Method: http.MethodGet, Path: "/tasks",
		Summary: "The task list", Subject: tasks}, httpx.SignedIn(),
		func(context.Context, page.Request, *struct{}) (page.View, error) {
			if *fail != nil {
				return page.View{}, *fail
			}
			return page.View{Title: "Tasks"}, nil
		})
	page.Serve(app, shell, page.Route{ID: "subject-read", Method: http.MethodGet, Path: "/tasks/{id}",
		Summary: "One task", Subject: record}, httpx.SignedIn(),
		func(context.Context, page.Request, *struct {
			ID string `path:"id"`
		}) (page.View, error) {
			if *fail != nil {
				return page.View{}, *fail
			}
			return page.View{Title: subjectRowTitle}, nil
		})
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the fixture does not describe itself: %v", err)
	}
	return router
}

func openSubject(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "http://"+refusalHost+path, nil)
	r.Header.Set("Accept", "text/html")
	r.AddCookie(&http.Cookie{Name: httpx.SessionCookie, Value: "present"})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// TestARecordThatIsGoneOrWithdrawnIsAnsweredAboutTheRecordWithAWayBackToItsList: a 404 and a
// 403 that names no grant, at a record page, say what happened to the item and link to the
// list it belongs to, and show nothing of the row.
func TestARecordThatIsGoneOrWithdrawnIsAnsweredAboutTheRecordWithAWayBackToItsList(t *testing.T) {
	var fail error
	h := subjectServer(t, &fail)
	if got := openSubject(t, h, subjectRecord); got.Code != http.StatusOK || !strings.Contains(got.Body.String(), subjectRowTitle) {
		t.Fatalf("GET %s = %d with a read that succeeds; this case is about the read that is refused", subjectRecord, got.Code)
	}

	for _, c := range []struct {
		status   int
		sentence string
	}{
		{http.StatusNotFound, "This item is no longer available."},
		{http.StatusForbidden, "You no longer have access to this item."},
	} {
		fail = &problem.Problem{Status: c.status, Detail: "no row answers this id for this caller"}
		got := openSubject(t, h, subjectRecord)
		body := got.Body.String()
		if got.Code != c.status {
			t.Errorf("a refused record read = %d, want %d", got.Code, c.status)
		}
		for _, want := range []string{c.sentence, "Back to tasks", `href="` + subjectList + `"`} {
			if !strings.Contains(body, want) {
				t.Errorf("a %d at a record omits %q\n%s", c.status, want, trim(body))
			}
		}
		for _, reject := range []string{subjectRowTitle, "no row answers this id", "Forbidden", "Not Found"} {
			if strings.Contains(body, reject) {
				t.Errorf("a %d at a record says %q\n%s", c.status, reject, trim(body))
			}
		}
	}
}
