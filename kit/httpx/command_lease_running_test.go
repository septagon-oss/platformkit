package httpx_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/idempotency"
)

func TestALeasedCommandStillRunningCannotCommitTwice(t *testing.T) {
	inside, release := make(chan struct{}), make(chan struct{})
	store := &agedClaim{Store: idempotency.Postgres(httpx.ConnFrom)}
	router, command := mountCommandEffect(t, store, "running-past-lease", func(db.Tx[db.Tenant]) gorm.ConnPool {
		close(inside)
		<-release
		return nil
	})
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- keyedPost(t, router, http.MethodPost, ambiguousPath("running-past-lease"), "running-past-lease", ambiguousBody)
	}()
	defer func() {
		close(release)
		select {
		case first := <-done:
			t.Logf("first command = %d %s", first.Code, first.Body.String())
		case <-time.After(15 * time.Second):
			t.Error("first command did not finish after release")
		}
		if rows := command.committedRows(t); rows != 1 {
			t.Errorf("one key committed %d command rows, want one", rows)
		}
	}()
	select {
	case <-inside:
	case first := <-done:
		t.Fatalf("first command ended before waiting: %d %s", first.Code, first.Body.String())
	case <-time.After(15 * time.Second):
		t.Fatal("first command did not enter its transaction")
	}
	store.elapsed = idempotency.Lease + time.Second
	retry := keyedPost(t, router, http.MethodPost, ambiguousPath("running-past-lease"), "running-past-lease", ambiguousBody)
	t.Logf("retry while original transaction runs = %d %s; command executions = %d", retry.Code, retry.Body.String(), command.ran.Load())
	if retry.Code == http.StatusConflict && !strings.Contains(retry.Body.String(), httpx.CodeIdempotencyInFlight) {
		t.Errorf("a running-command refusal names no in-flight reason: %s", retry.Body.String())
	}
}
