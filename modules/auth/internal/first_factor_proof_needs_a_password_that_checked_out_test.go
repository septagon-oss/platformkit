package internal_test

// What this file pins: the window that makes a second factor spendable is minted
// by a first factor that *checked out*, and by nothing that merely saw the address.
//
// `000033_first_factor_proofs` answers the question "did this caller ever offer
// the first proof", and the answer it consults is a row written by
// `markFirstFactorProved`. That one call site per door is the whole of the cure:
// a window minted wherever an address was merely *tried* would put this module
// back exactly where the challenge route was found to be — a code held by
// a stranger being the account — because every public door in this module knows
// how to take an address. So the risk that matters here is not the door that was
// closed, it is a mint attached to the wrong branch: the wrong password, the
// address nobody has, the forgotten-password link that answers everybody the same
// way. Each of those three sees the address and proves nothing about it.
//
// The case therefore asks each of them in turn and, between them and the
// challenge, spends nothing: the refusals must leave no window behind, so the
// person's own unspent recovery code still signs nobody in. Then it types the
// password and answers with that same code, which must work — the assertion that
// makes the refusals above a claim about the window rather than about the code.
//
// Nothing here reads a sentence a refusal prints, and no status the module could
// answer some other way is pinned: a first factor that did not check out is "a
// 4xx that is not a 5xx and sets no cookie", and what settles whether a window
// exists is the 200 that a real one earns. Every count is read off the ledger —
// live sessions, unspent codes, the outbox — before and after.

import (
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/user"
)

func TestASignInThatNeverCheckedOutThePasswordMintsNoWindow(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	router, _, _, api := mountRecorded(t, conn, auth.OIDC{}, false, func(d *auth.Deps) {
		d.FactorKey = "a factor key this deployment set"
	})

	// The challenge's address is read off the composition, so a cure that moves
	// the leg keeps having to answer this case.
	challenge := ""
	for _, m := range api.Mounted() {
		if m.Module == "auth" && m.Method == http.MethodPost && strings.Contains(m.Path, "challenge") {
			challenge = m.Path
		}
	}
	if challenge == "" {
		t.Fatal("the composition mounts no auth second-factor challenge: this case has no door to ask")
	}

	const email = "ada@acme.localhost"
	ada := person(t, conn, email, contracts.RoleAdmin)
	_, codes := enrolByRoute(t, router, conn, email)
	if len(codes) < 3 {
		t.Fatalf("the enrolment issued %d recovery codes, want enough for one answer each", len(codes))
	}

	// The arrangement, and the proof that this case knows how to open a window:
	// the password typed at an account that carries a factor is held at the door
	// with no session, and that holding is what makes one code spendable. The
	// answer that follows is the reading — a 200 with a cookie means a window was
	// minted by a password that checked out, which is the half this case then
	// takes away.
	held := call(t, router, http.MethodPost, "/api/v1/auth/login",
		`{"email":"`+email+`","password":"`+authtest.Password+`"}`)
	if held.Code < http.StatusBadRequest || held.Code >= http.StatusInternalServerError {
		t.Fatalf("the password alone at an account that carries a factor = %d %s, want a refusal",
			held.Code, held.Body.String())
	}
	if sessionCookie(held) != "" {
		t.Fatal("the password alone opened a session at an account that carries a factor")
	}
	opened := call(t, router, http.MethodPost, challenge,
		`{"email":"`+email+`","code":"`+codes[0]+`"}`)
	if opened.Code != http.StatusOK || sessionCookie(opened) == "" {
		t.Fatalf("a recovery code offered behind a password that checked out = %d %s, cookie %q: "+
			"this case cannot say anything about a window it cannot open",
			opened.Code, opened.Body.String(), sessionCookie(opened))
	}

	liveBefore, unusedBefore, eventsBefore := proofLedger(t, conn, ada)
	if unusedBefore != len(codes)-1 {
		t.Fatalf("%d recovery codes are unspent after the arrangement, want %d: this case spends one of its own",
			unusedBefore, len(codes)-1)
	}

	// Three doors that see the address and prove nothing about it.
	wrong := call(t, router, http.MethodPost, "/api/v1/auth/login",
		`{"email":"`+email+`","password":"a password this person never set"}`)
	if wrong.Code < http.StatusBadRequest || wrong.Code >= http.StatusInternalServerError {
		t.Errorf("a wrong password = %d %s, want a refusal", wrong.Code, wrong.Body.String())
	}
	if sessionCookie(wrong) != "" {
		t.Error("a wrong password set a session cookie")
	}
	ghost := call(t, router, http.MethodPost, "/api/v1/auth/login",
		`{"email":"nobody@acme.localhost","password":"a password nobody set"}`)
	if ghost.Code < http.StatusBadRequest || ghost.Code >= http.StatusInternalServerError {
		t.Errorf("an address nobody has = %d %s, want a refusal", ghost.Code, ghost.Body.String())
	}
	if sessionCookie(ghost) != "" {
		t.Error("an address nobody has set a session cookie")
	}
	forgot := call(t, router, http.MethodPost, "/api/v1/auth/password/forgot",
		`{"email":"`+email+`"}`)
	if forgot.Code >= http.StatusInternalServerError {
		t.Errorf("a forgotten-password link = %d %s, want the same answer either way and never a fault",
			forgot.Code, forgot.Body.String())
	}
	if sessionCookie(forgot) != "" {
		t.Error("asking for a reset link set a session cookie")
	}

	// None of them earned anything, so this person's own code is still unspent
	// and still signs nobody in.
	spent := call(t, router, http.MethodPost, challenge,
		`{"email":"`+email+`","code":"`+codes[1]+`"}`)
	if spent.Code == http.StatusOK || spent.Code == http.StatusCreated {
		t.Errorf("answering %s with a recovery code behind a wrong password, an unknown address and a "+
			"reset request = %d %s, want a refusal: the address these three offered is not a fact about "+
			"the person behind it, and a window minted by any of them is the account for whoever knows a code",
			challenge, spent.Code, spent.Body.String())
	}
	if sessionCookie(spent) != "" {
		t.Errorf("a recovery code offered with no first factor checked out set a session cookie at %s",
			challenge)
	}
	liveAfter, unusedAfter, eventsAfter := proofLedger(t, conn, ada)
	if liveAfter != liveBefore {
		t.Errorf("the ledger holds %d live sessions for %s, want the %d held before: a refused mutation "+
			"writes no row", liveAfter, email, liveBefore)
	}
	if unusedAfter != unusedBefore {
		t.Errorf("%d recovery codes are unspent after the refusal, want the %d before it: a refusal that "+
			"spends a code loses the code and records a sign-in that did not happen", unusedAfter, unusedBefore)
	}
	if got, want := countNamed(eventsAfter, contracts.EventLoggedIn), countNamed(eventsBefore, contracts.EventLoggedIn); got != want {
		t.Errorf("the outbox holds %d %s rows where it held %d", got, contracts.EventLoggedIn, want)
	}
	if got, want := countNamed(eventsAfter, contracts.EventRecoveryCodeUsed), countNamed(eventsBefore, contracts.EventRecoveryCodeUsed); got != want {
		t.Errorf("the outbox holds %d %s rows where it held %d", got, contracts.EventRecoveryCodeUsed, want)
	}

	// The same code, one minute later, behind the password that is right. This is
	// what makes the refusals above a claim about the window: the code was
	// spendable the whole time, and only the fact in front of it changed.
	again := call(t, router, http.MethodPost, "/api/v1/auth/login",
		`{"email":"`+email+`","password":"`+authtest.Password+`"}`)
	if sessionCookie(again) != "" {
		t.Fatalf("the password alone opened a session at an account that carries a factor")
	}
	finished := call(t, router, http.MethodPost, challenge,
		`{"email":"`+email+`","code":"`+codes[1]+`"}`)
	if finished.Code != http.StatusOK || sessionCookie(finished) == "" {
		t.Errorf("the same code behind a password that checked out = %d %s, cookie %q, want 200 and a session: "+
			"a refusal that outlives the door that owed it is a lock-out, not a control",
			finished.Code, finished.Body.String(), sessionCookie(finished))
	}
	liveNow, unusedNow, eventsNow := proofLedger(t, conn, ada)
	if liveNow != liveAfter+1 {
		t.Errorf("the sign-in that offered both halves left %d live sessions, want %d", liveNow, liveAfter+1)
	}
	if unusedNow != unusedAfter-1 {
		t.Errorf("%d recovery codes are unspent after the sign-in that earned it, want %d", unusedNow, unusedAfter-1)
	}
	if got, want := countNamed(eventsNow, contracts.EventLoggedIn), countNamed(eventsAfter, contracts.EventLoggedIn)+1; got != want {
		t.Errorf("the trail holds %d %s rows for a sign-in that opened a session, want %d",
			got, contracts.EventLoggedIn, want)
	}
	if got, want := countNamed(eventsNow, contracts.EventRecoveryCodeUsed), countNamed(eventsAfter, contracts.EventRecoveryCodeUsed)+1; got != want {
		t.Errorf("the trail holds %d %s rows for a code this sign-in spent, want %d",
			got, contracts.EventRecoveryCodeUsed, want)
	}
}
