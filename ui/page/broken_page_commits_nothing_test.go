package page_test

// A page that wrote in its request's transaction and then failed is drawn as this package's
// failure page rather than handed to the kernel as an error. The kernel decides commit or
// rollback on the status the response carries, so the page must carry the failure's own
// status: a refused or broken request commits nothing it wrote.

import (
	"context"
	"errors"
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

// TestAPageWhoseWriteFailedAnswersItsOwnStatusAndCommitsNothing: the same page first writes and
// succeeds, so the row count proves the write reaches the database; then it writes and fails
// with a server error and with a refusal, each answered as a page at its own status, and
// neither row survives the request.
func TestAPageWhoseWriteFailedAnswersItsOwnStatusAndCommitsNothing(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	if _, err := admin.ExecContext(t.Context(),
		`CREATE TABLE notes (id serial PRIMARY KEY, tenant_id uuid NOT NULL, body text NOT NULL)`); err != nil {
		t.Fatalf("create notes: %v", err)
	}
	tenant := tenancy.Tenant{ID: uuid.MustParse("11111111-1111-4111-8111-111111111111"), Slug: "acme", Name: "Acme"}
	count := func() int {
		t.Helper()
		var n int
		if err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
			return tx.DB().Raw("SELECT count(*) FROM notes").Row().Scan(&n)
		}); err != nil {
			t.Fatalf("count notes: %v", err)
		}
		return n
	}

	s := shell()
	api, router := httpx.New(httpx.Options{
		Cache: cache.Memory("pkit"), PublicHost: refusalHost, Tenants: refusalDirectory{},
		Authorize: refusalAuthorizer{allow: true}, Conn: conn, Log: slog.New(slog.DiscardHandler),
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: uuid.MustParse("22222222-2222-4222-8222-222222222222")}, true, nil
		},
		Fault: page.FaultHandler(s),
	})
	var fail error
	page.Serve(api.Surfaces("task").App, s, page.Route{ID: "note-write", Method: http.MethodPost, Path: "/tasks",
		Summary: "Write a note", Subject: page.Subject{Singular: "task", Plural: "tasks", List: subjectList}},
		httpx.SignedIn(), func(ctx context.Context, _ page.Request, _ *struct{}) (page.View, error) {
			tx, ok := httpx.TxFrom(ctx)
			if !ok {
				return page.View{}, errors.New("no request transaction")
			}
			if err := tx.DB().Exec("INSERT INTO notes (tenant_id, body) VALUES (?, ?)", tenant.ID.String(), "written").Error; err != nil {
				return page.View{}, err
			}
			if fail != nil {
				return page.View{}, fail
			}
			return page.View{Title: "Tasks"}, nil
		})
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the fixture does not describe itself: %v", err)
	}
	post := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "http://"+refusalHost+subjectList, nil)
		r.Header.Set("Accept", "text/html")
		r.Header.Set("Origin", "http://"+refusalHost)
		r.Header.Set("Sec-Fetch-Site", "same-origin")
		r.AddCookie(&http.Cookie{Name: httpx.SessionCookie, Value: "present"})
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}

	if got := post(); got.Code != http.StatusOK || count() != 1 {
		t.Fatalf("a write that succeeds: status=%d notes=%d, want 200 and 1\n%s", got.Code, count(), trim(got.Body.String()))
	}
	// A module's own 4xx sentence is the writer's line and stands on the page (see plainWords),
	// so only the server failure's cause is held off it.
	for _, c := range []struct {
		name    string
		err     error
		status  int
		rejects []string
	}{
		{"server failure", errors.New("the connection to the database was reset"), http.StatusInternalServerError,
			[]string{"connection to the database", "Internal Server Error"}},
		{"refusal", &problem.Problem{Status: http.StatusConflict, Detail: "the row moved under this write"}, http.StatusConflict, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			fail = c.err
			got := post()
			body := got.Body.String()
			if got.Code != c.status {
				t.Errorf("a write that failed = %d, want %d\n%s", got.Code, c.status, trim(body))
			}
			if !strings.Contains(got.Header().Get("Content-Type"), "text/html") {
				t.Errorf("a write that failed is answered as %q, not a page", got.Header().Get("Content-Type"))
			}
			for _, reject := range c.rejects {
				if strings.Contains(body, reject) {
					t.Errorf("a write that failed says %q\n%s", reject, trim(body))
				}
			}
			if n := count(); n != 1 {
				t.Errorf("%d notes after a write that failed with %d, want the 1 the successful write left", n, c.status)
			}
		})
	}
}
