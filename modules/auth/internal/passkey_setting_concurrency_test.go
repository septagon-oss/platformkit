package internal_test

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/user"
)

func TestConcurrentPasskeySignInSettingsPublishOneTransition(t *testing.T) {
	admin, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	router, _, _ := mountOn(t, conn, auth.OIDC{})
	person(t, conn, "ada@acme.localhost", contracts.RoleAdmin)
	session := signIn(t, router, "ada@acme.localhost")
	setDoor(t, router, session, true)
	setDoor(t, router, session, false)
	before := doorEvents(t, conn)

	blocker, err := admin.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback()
	var holder int
	if err := blocker.QueryRowContext(t.Context(), "SELECT pg_backend_pid() FROM passkey_settings FOR UPDATE").Scan(&holder); err != nil {
		t.Fatal(err)
	}

	responses := make([]*httptest.ResponseRecorder, 2)
	var workers sync.WaitGroup
	for i := range responses {
		workers.Go(func() {
			responses[i] = call(t, router, http.MethodPost, door, `{"enabled":true}`, withSession(session))
		})
	}
	// A lock before the policy read and a guarded update both satisfy this barrier.
	// Wait for the two requests, including a contender queued behind the first.
	queued := 0
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		err = admin.QueryRowContext(t.Context(), `SELECT count(*) FROM pg_stat_activity a
 WHERE $1 = ANY(pg_blocking_pids(a.pid)) OR EXISTS (
  SELECT 1 FROM unnest(pg_blocking_pids(a.pid)) b(pid)
  WHERE $1 = ANY(pg_blocking_pids(b.pid)))`, holder).Scan(&queued)
		if err != nil || queued >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	releaseErr := blocker.Rollback()
	workers.Wait()
	if err != nil {
		t.Fatal(err)
	}
	if releaseErr != nil {
		t.Fatal(releaseErr)
	}
	if queued < 2 {
		t.Fatalf("only %d setting commands queued behind the setting row", queued)
	}
	for i, response := range responses {
		if response.Code != http.StatusOK {
			t.Errorf("setting request %d = %d %s, want 200", i, response.Code, response.Body.String())
		}
	}
	if delta := doorEvents(t, conn) - before; delta != 1 {
		t.Errorf("two requests enabling one disabled door published %d transitions, want 1", delta)
	}
	if begun := call(t, router, http.MethodPost, "/api/v1/auth/login/passkey/begin", ""); begun.Code != http.StatusOK {
		t.Errorf("the enabled door = %d %s, want 200", begun.Code, begun.Body.String())
	}
}
