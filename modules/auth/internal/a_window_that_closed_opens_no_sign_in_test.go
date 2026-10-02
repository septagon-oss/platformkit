package internal_test

// The five minutes, enforced by the statement that spends the window — not only
// by the sweep that eventually deletes the row.
//
// contracts.FirstFactorProofWindow is the lifetime of the fact "this account's
// first factor checked out and its second half is still outstanding". README and
// migration agree about why it is short:
//
//   modules/auth/README.md — the row is open for contracts.FirstFactorProofWindow,
//       "and no wider, because the only way to get another window is to type the
//       first factor again".
//   000033_first_factor_proofs.up.sql — "Five minutes … because the number is
//       enforced by the statement that reads this table and a deployment that
//       widened it would have weakened itself".
//
// The statement that reads it is one DELETE in Service.RequireFirstFactorProof:
//
//   DELETE FROM first_factor_proofs WHERE user_id = ? AND expires_at > now()
//
// and the expiry half of that WHERE is the whole of the enforcement. The row's
// own removal is the hourly Purge's job, which TestThePurgeTakesAnAgedWindow
// already pins, so between a window closing and the sweep walking past, the only
// thing standing between a correct code and a session is `expires_at > now()`.
// Nothing in this tree ever offered a code inside that gap. This case does: a
// refused sign-in, a window aged past its five minutes, one correct recovery code
// the account still holds, and the ledger read around the attempt.
//
// It fails under the mutation it is there to catch — the predicate dropped to
// `WHERE user_id = ?`, which leaves the whole `modules/auth/internal` suite green
// today — and it passes with the predicate in place, which is what it does at this
// head. Every assertion is a row, a cookie or an outbox name; none of them reads a
// refusal's wording.

import (
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/user"
)

// occurrences is how many outbox rows of one name the trail holds. proofLedger
// hands back the whole outbox, so the assertion is the difference this attempt
// made, never the presence of a name an earlier enrolment already wrote.
func occurrences(events []string, name string) int {
	var n int
	for _, e := range events {
		if e == name {
			n++
		}
	}
	return n
}

func TestAWindowThatClosedOpensNoSignInBeforeTheSweepTakesIt(t *testing.T) {
	admin, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	router, _, _, api := mountRecorded(t, conn, auth.OIDC{}, false, func(d *auth.Deps) {
		d.FactorKey = "a factor key this deployment set"
	})
	challenge := ""
	for _, m := range api.Mounted() {
		if m.Module == "auth" && m.Method == http.MethodPost && strings.Contains(m.Path, "challenge") {
			challenge = m.Path
		}
	}
	if challenge == "" {
		t.Fatal("the composition mounts no auth-challenge-verify operation")
	}
	const email = "ada@acme.localhost"
	ada := person(t, conn, email, contracts.RoleAdmin)
	_, codes := enrolByRoute(t, router, conn, email)

	// The password alone is refused, and that refusal is what opens the window.
	if res := call(t, router, http.MethodPost, "/api/v1/auth/login",
		`{"email":"`+email+`","password":"`+authtest.Password+`"}`); res.Code != http.StatusUnauthorized {
		t.Fatalf("the password alone, with a factor enrolled = %d %s, want 401", res.Code, res.Body.String())
	}
	if rows := proofRows(t, conn, ada); rows != 1 {
		t.Fatalf("%d windows are open after a refused sign-in, want 1", rows)
	}

	// The window closes. The sweep has not run: the row is still on the table,
	// which is exactly the interval in which only the SQL says "too late".
	exec(t, admin, `UPDATE first_factor_proofs SET expires_at = now() - interval '1 second'`)
	if rows := proofRows(t, conn, ada); rows != 1 {
		t.Fatalf("%d windows are on the table after ageing, want the 1 still awaiting its sweep", rows)
	}

	live, spare, before := proofLedger(t, conn, ada)
	opened := occurrences(before, contracts.EventLoggedIn)
	closed := call(t, router, http.MethodPost, challenge,
		`{"email":"`+email+`","code":"`+codes[0]+`"}`)
	if closed.Code == http.StatusOK || closed.Code == http.StatusCreated {
		t.Errorf("a recovery code answered after the window closed = %d %s, want a refusal: with the "+
			"expiry gone from the spend, a stolen code is a sign-in for as long as the hourly sweep takes "+
			"to walk past the row (%s)", closed.Code, closed.Body.String(), contracts.FirstFactorProofWindow)
	}
	if sessionCookie(closed) != "" {
		t.Error("the answer given after the window closed set a session cookie")
	}

	afterLive, afterSpare, after := proofLedger(t, conn, ada)
	if afterSpare != spare {
		t.Errorf("%d recovery codes are unused after the refusal, want the %d held before it: a refusal "+
			"may not spend the credential it turned away", afterSpare, spare)
	}
	if afterLive != live {
		t.Errorf("%d sessions are live for %s after the refusal, want the %d held before it",
			afterLive, email, live)
	}
	if got := occurrences(after, contracts.EventLoggedIn); got != opened {
		t.Errorf("%d auth.logged_in events are on the trail where %d were before the attempt: a refusal "+
			"emits nothing, and a closed window that still signs people in is a stolen code with a clock on it",
			got, opened)
	}
	if rows := proofRows(t, conn, ada); rows != 1 {
		t.Errorf("%d windows are left after a refusal, want the 1 it refused to spend: the spend is the "+
			"DELETE, and a refusal that deletes took something back", rows)
	}
	_ = db.Now
}
