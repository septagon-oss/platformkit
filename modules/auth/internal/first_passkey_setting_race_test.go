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

// TestTheFirstPasskeySignInSettingPublishesOneTransition is the half of the
// setting's race a row lock cannot cover: a tenant with no passkey_settings row
// yet. Both requests read "no row" under FOR UPDATE, so only the write's guard
// can keep the trail at one transition. An uncommitted insert of the same key
// holds both requests at the unique index; rolling it back lets them race each
// other for the first row.
func TestTheFirstPasskeySignInSettingPublishesOneTransition(t *testing.T) {
	admin, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	router, _, _ := mountOn(t, conn, auth.OIDC{})
	person(t, conn, "ada@acme.localhost", contracts.RoleAdmin)
	session := signIn(t, router, "ada@acme.localhost")
	before := doorEvents(t, conn)

	blocker, err := admin.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback()
	// The blocker writes on the fixture's own admin connection, which is the one
	// role in this repository that reads with row security bypassed, so it names
	// no tenant setting: scripts/check_gucs.sh refuses a Go file outside kit/db
	// that names one, and the setting was never what held the two requests. What
	// holds them is the table's unique index, which no policy filters — the row
	// below stays uncommitted, and the requests queue behind it there.
	var holder int
	if err := blocker.QueryRowContext(t.Context(), "SELECT pg_backend_pid()").Scan(&holder); err != nil {
		t.Fatal(err)
	}
	if _, err := blocker.ExecContext(t.Context(),
		"INSERT INTO passkey_settings (tenant_id, sign_in) VALUES ($1, true)", acme.ID); err != nil {
		t.Fatal(err)
	}

	responses := make([]*httptest.ResponseRecorder, 2)
	var workers sync.WaitGroup
	for i := range responses {
		workers.Go(func() {
			responses[i] = call(t, router, http.MethodPost, door, `{"enabled":true}`, withSession(session))
		})
	}
	queued := 0
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		err = admin.QueryRowContext(t.Context(),
			`SELECT count(*) FROM pg_stat_activity WHERE $1 = ANY(pg_blocking_pids(pid))`, holder).Scan(&queued)
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
		t.Fatalf("only %d setting commands queued behind the uncommitted first row", queued)
	}
	for i, response := range responses {
		if response.Code != http.StatusOK {
			t.Errorf("setting request %d = %d %s, want 200", i, response.Code, response.Body.String())
		}
	}
	if delta := doorEvents(t, conn) - before; delta != 1 {
		t.Errorf("two requests opening a door that had no row published %d transitions, want 1", delta)
	}
	if begun := call(t, router, http.MethodPost, "/api/v1/auth/login/passkey/begin", ""); begun.Code != http.StatusOK {
		t.Errorf("the enabled door = %d %s, want 200", begun.Code, begun.Body.String())
	}
}
