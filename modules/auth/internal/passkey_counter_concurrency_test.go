package internal_test

import (
	"encoding/json"
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

func TestConcurrentPasskeyAssertionsRejectARepeatedCounter(t *testing.T) {
	admin, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	router, _, _ := mountOn(t, conn, auth.OIDC{})
	ada := person(t, conn, "ada@acme.localhost", contracts.RoleAdmin)
	session := signIn(t, router, "ada@acme.localhost")
	key := newSoftAuthenticator(t, host)
	key.counter = 1
	if code, body := enrolPasskey(t, router, session, key); code != http.StatusCreated {
		t.Fatalf("enrol = %d %s", code, body)
	}
	setDoor(t, router, session, true)
	bodies := make([]string, 2)
	key.counter = 2
	for i := range bodies {
		begun := call(t, router, http.MethodPost, "/api/v1/auth/login/passkey/begin", "")
		if begun.Code != http.StatusOK {
			t.Fatalf("begin = %d %s", begun.Code, begun.Body.String())
		}
		ceremony, challenge := challengeOf(t, begun.Body.Bytes())
		body, err := json.Marshal(map[string]any{"ceremony": ceremony, "response": key.asserted(t, challenge)})
		if err != nil {
			t.Fatal(err)
		}
		bodies[i] = string(body)
	}
	live, _, before := proofLedger(t, conn, ada)
	blocker, err := admin.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback()
	var holder int
	if err := blocker.QueryRowContext(t.Context(), "SELECT pg_backend_pid() FROM passkey_credentials FOR UPDATE").Scan(&holder); err != nil {
		t.Fatal(err)
	}
	responses := make([]*httptest.ResponseRecorder, 2)
	var wg sync.WaitGroup
	for i := range bodies {
		wg.Go(func() {
			responses[i] = call(t, router, http.MethodPost, "/api/v1/auth/login/passkey/verify", bodies[i])
		})
	}
	// Both requests must reach a database lock before releasing it. This also
	// permits a corrected implementation to lock the row before validating it.
	deadline := time.Now().Add(10 * time.Second)
	queued := 0
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
	if releaseErr := blocker.Rollback(); releaseErr != nil {
		t.Fatal(releaseErr)
	}
	wg.Wait()
	if err != nil {
		t.Fatal(err)
	}
	if queued < 2 {
		t.Fatalf("only %d assertions queued behind the credential lock", queued)
	}
	accepted := 0
	for _, res := range responses {
		if res.Code == http.StatusOK && sessionCookie(res) != "" {
			accepted++
		}
	}
	if accepted != 1 {
		t.Errorf("same nonzero counter on two distinct ceremonies: statuses %d, %d; accepted %d sessions, want 1", responses[0].Code, responses[1].Code, accepted)
	}
	afterLive, _, after := proofLedger(t, conn, ada)
	if afterLive != live+1 {
		t.Errorf("session delta = %d, want 1", afterLive-live)
	}
	for _, event := range []string{contracts.EventLoggedIn, contracts.EventFactorUsed} {
		if delta := occurrences(after, event) - occurrences(before, event); delta != 1 {
			t.Errorf("%s delta = %d, want 1", event, delta)
		}
	}
}
