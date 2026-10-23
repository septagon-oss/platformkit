package task_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/task"
	"github.com/septagon-oss/platformkit/modules/task/contracts"
	"github.com/septagon-oss/platformkit/modules/task/internal"
)

// quietCreator inserts the row and publishes nothing, so any event in the
// outbox after a create came from the door and not from the creator.
type quietCreator struct {
	*internal.Service
	calls int
}

func (q *quietCreator) Create(ctx context.Context, tx db.Tx[db.Tenant], in contracts.CreateTask) (*contracts.Task, error) {
	q.calls++
	row := &contracts.Task{Title: in.Title, Status: contracts.StatusOpen, Priority: contracts.PriorityNormal}
	row.ID = uuid.New()
	return row, crud.Create(ctx, tx, row)
}

// TestTheJSONCreateReachesTheCreatorAndPublishesNothingItself.
func TestTheJSONCreateReachesTheCreatorAndPublishesNothingItself(t *testing.T) {
	admin, conn := dbtest.Schema(t, task.Migrations)
	spy := &quietCreator{Service: internal.NewService()}
	api, router := httpx.New(httpx.Options{
		Cache: cache.Memory("pkit"), PublicHost: host, Tenants: caller{}, Conn: conn, Authorize: caller{},
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: uuid.New()}, true, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	task.New(task.Deps{Service: spy}).Routes(surfacesOf(api))
	req := httptest.NewRequest(http.MethodPost, "http://"+host+path, strings.NewReader(`{"title":"x"}`))
	req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST = %d %s", w.Code, w.Body)
	}
	if spy.calls != 1 {
		t.Errorf("the creator was reached %d times, want 1", spy.calls)
	}
	var n int
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM platformkit_outbox").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("the outbox holds %d events, want 0: the creator published none", n)
	}
}
