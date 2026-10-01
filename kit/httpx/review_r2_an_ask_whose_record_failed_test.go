package httpx_test

// review_r2_an_ask_whose_record_failed_test.go is the second review's pin over the
// one verdict of an ask that no other case on this branch drives: the record itself
// failing.
//
// Commit 6e1500e cured the first review's finding 9 by giving httpx.Options.Accessed
// an error and returning it, and wrote the property into the command's own comment:
// "an ask whose event did not commit rolls back with its notices". Putting the drop
// back — `_ = d.record(ctx, …)` — left every case in this package and every case in
// apps/platformkit green, which is how its author knows nothing consults the branch.
// These two cases are the difference between a comment and a gate.
//
// The first asks what the door answers when recording fails: a verdict the caller can
// act on, not the 202 of an ask nobody was told about. The second keeps the healthy
// path honest, so the first cannot be satisfied by a command that fails every ask.
//
// The notices go into the caller's own transaction, which kit/httpx's transaction.go
// commits only when the handler answered without an error, so a failure that reaches
// the caller *is* the rollback. At this layer the observable is therefore the verdict;
// the database half of the claim — no notice left in the bell — is read back by
// apps/platformkit/TestAnAskWhoseNoticeFailsLeavesNoHalfWrittenAsk over the real reach.

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// askSetupRecord is askSetup with the one thing it cannot answer: a record that
// fails. The reach is a healthy one, so the only failure in the request is the trail.
func askSetupRecord(t *testing.T, recErr error) (http.Handler, string, *fixture, *int) {
	t.Helper()
	_, app := dbtest.Schema(t)
	f := &fixture{
		tenant: tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"},
		app:    app,
		logs:   &lines{},
	}
	attempted := new(int)
	api, router := httpx.New(httpx.Options{
		Installation: host,
		PublicHost:   host,
		Tenants:      f,
		Conn:         app,
		Authorize:    f,
		Entitle:      f,
		Authenticate: f.authenticate,
		Log:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		Access:       &reachF{recipients: []uuid.UUID{uuid.New()}},
		Accessed: func(_ context.Context, _ httpx.AccessRecord) error {
			*attempted++
			return recErr
		},
		WriteLimiter: limiterF{allowed: true},
	})
	api.Declare([]tenancy.Grant{{Permission: "widget:read", Label: "read widgets"}})
	httpx.Register(api.Surfaces(probe).App, huma.Operation{
		OperationID: "ask-widget-record", Method: http.MethodPost, Path: "/asks",
		DefaultStatus: http.StatusAccepted,
	}, httpx.SignedIn(), func(ctx context.Context, in *askInput) (*struct{}, error) {
		return nil, httpx.Ask(ctx, httpx.AccessAsk{Permission: in.Body.Permission, Path: in.Body.Path})
	})
	return router, at(api, "/asks"), f, attempted
}

func TestAnAskWhoseRecordFailedIsNotAnsweredAsSent(t *testing.T) {
	const asked = `{"permission":"widget:read","path":"/app/widget/widgets"}`

	t.Run("a trail that refuses the ask fails it", func(t *testing.T) {
		h, path, f, attempted := askSetupRecord(t, errors.New("the outbox is not accepting events"))
		f.signedIn()
		f.allow = true

		res := askOnce(t, h, path, asked)
		if *attempted != 1 {
			t.Fatalf("the command asked the trail %d times, want exactly one attempt", *attempted)
		}
		if res.Code < 400 {
			t.Errorf("POST the ask with a failing record = %d %s, want a failure the caller can act on: an ask whose event did not commit is not an ask that was sent",
				res.Code, res.Body)
		}
	})

	t.Run("a trail that takes the ask answers it", func(t *testing.T) {
		h, path, f, attempted := askSetupRecord(t, nil)
		f.signedIn()
		f.allow = true

		res := askOnce(t, h, path, asked)
		if res.Code != http.StatusAccepted || *attempted != 1 {
			t.Errorf("the healthy ask = %d after %d record attempt(s), want 202 and one attempt: %s",
				res.Code, *attempted, res.Body)
		}
	})
}
