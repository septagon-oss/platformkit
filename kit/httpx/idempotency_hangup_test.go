package httpx_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// TestACommandWhoseCallerHungUpBeforeTheCommitRunsOnItsRetry: the caller that
// went away while its command was running is the caller this mechanism exists
// for, and it comes back with the same key. If the hang-up rolled the command
// back, nothing happened, and the retry must run it — never be handed the answer
// the handler wrote for a transaction that did not commit.
func TestACommandWhoseCallerHungUpBeforeTheCommitRunsOnItsRetry(t *testing.T) {
	api, router, f, _, _ := setupNote(t)
	var runs atomic.Int64
	var hangUp atomic.Pointer[context.CancelFunc]
	op := huma.Operation{OperationID: "abandoned-note", Method: http.MethodPost, Path: "/abandoned"}
	httpx.DeclareIdempotency(&op)
	httpx.Register(api.Surfaces(probe).App, op, httpx.Permission("note:write"),
		func(ctx context.Context, in *noteIn) (*noteOut, error) {
			n := runs.Add(1)
			tx, ok := httpx.TxFrom(ctx)
			if !ok {
				return nil, errors.New("the command was run outside a request")
			}
			tn, _ := tenancy.FromContext(ctx)
			if err := tx.DB().Exec("INSERT INTO notes (tenant_id, body) VALUES (?, ?)", tn.ID, in.Body.Text).Error; err != nil {
				return nil, err
			}
			// The caller goes away after the work and before the commit.
			if cancel := hangUp.Swap(nil); cancel != nil {
				(*cancel)()
			}
			out := &noteOut{}
			out.Body.Text, out.Body.Runs = in.Body.Text, n
			return out, nil
		})
	path := at(api, "/abandoned")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	hangUp.Store(&cancel)
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "http://"+host+path, strings.NewReader(`{"text":"left"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(httpx.IdempotencyKeyHeader, keyA)
	req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
	router.ServeHTTP(httptest.NewRecorder(), req)

	retry := send(t, router, path, keyA, "left")
	if retry.Code != http.StatusOK {
		t.Fatalf("the retry answered %d, want 200: %s", retry.Code, retry.Body)
	}
	if got := countRows(t, f, "notes"); got != 1 {
		t.Fatalf("after a hang-up and a retry under one key the notes table holds %d rows, want 1 (replay %q, handler runs %d)",
			got, retry.Header().Get(httpx.IdempotencyReplayHeader), runs.Load())
	}
}
