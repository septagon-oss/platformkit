package internal_test

// What this file pins: one RFC 6238 step is one sign-in, at the route a person
// actually answers, and the refusal of its replay costs nothing.
//
// `factors.go:401` says the spend "moves last_step, which refuses a replay of the
// same step", and `000031_auth_factors` writes `last_step` for no other reason:
// RFC 6238 §5.2 requires a verifier to accept a code at most once, because the
// five-minute transmission skew it allows is otherwise five minutes during which
// anybody who saw the code — a shoulder, a screenshot of the authenticator, a
// notification mirror — is in.
//
// The proof this claim used to have arrived by timing: two tabs answering one
// live step at the challenge route, one session. That shape stopped reaching the
// statement when the challenge began to spend a first-factor window first, since
// the tab without a refused sign-in behind it is turned away before the UPDATE
// (`review_r5_two_tabs_answering_one_code_test.go`'s header says so, and
// `first_half_test.go:313` took the shape on at the service, where it honestly
// states that it cannot tell the guarded UPDATE from the comparison above it).
// What was left unowned was the simplest reading of the claim, which needs no
// concurrency at all: answer a step, be signed in; answer *that same step* again
// behind a fresh refusal, be refused — and be refused having spent nothing, so a
// replay attempt does not also cost the person their recovery codes.
//
// The refusal is asserted through the ledger, never through a sentence: sessions
// and the outbox before and after, and the reachability of the door is proved by
// the sign-in that opens it and the recovery code that still opens it afterwards.

import (
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/user"
)

func TestOneTotpStepOpensOneSignInAndItsReplayOpensNone(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	router, _, _, _ := mountRecorded(t, conn, auth.OIDC{}, false, func(d *auth.Deps) {
		d.FactorKey = "a factor key this deployment set"
	})
	const (
		challenge = "/api/v1/auth/challenge/verify"
		email     = "ada@acme.localhost"
	)
	ada := person(t, conn, email, contracts.RoleAdmin)
	secret, codes := enrolByRoute(t, router, conn, email)

	// The door, opened once: the refused password, then the step it was waiting
	// for. This is the reachability guard — every claim below is about a code this
	// same case just watched open a session.
	halted := call(t, router, http.MethodPost, "/api/v1/auth/login",
		`{"email":"`+email+`","password":"`+authtest.Password+`"}`)
	if halted.Code < http.StatusBadRequest || halted.Code >= http.StatusInternalServerError ||
		sessionCookie(halted) != "" {
		t.Fatalf("the password alone at an account that carries a factor = %d %s, cookie %q, want a refusal "+
			"that opens nothing", halted.Code, halted.Body.String(), sessionCookie(halted))
	}
	step := codeFor(t, secret, db.Now())
	first := call(t, router, http.MethodPost, challenge, `{"email":"`+email+`","code":"`+step+`"}`)
	if first.Code != http.StatusOK || sessionCookie(first) == "" {
		t.Fatalf("a current step behind a refused sign-in = %d %s, cookie %q, want 200 and a session: "+
			"this case cannot say anything about a step the door would not take",
			first.Code, first.Body.String(), sessionCookie(first))
	}

	liveBefore, unusedBefore, eventsBefore := proofLedger(t, conn, ada)
	signedInBefore := countNamed(eventsBefore, contracts.EventLoggedIn)

	// The same step, from a caller who did everything right the second time too:
	// the password again, so a fresh window stands behind the answer. What is
	// wrong here is only the code, and it is wrong because it was already used.
	again := call(t, router, http.MethodPost, "/api/v1/auth/login",
		`{"email":"`+email+`","password":"`+authtest.Password+`"}`)
	if again.Code < http.StatusBadRequest || again.Code >= http.StatusInternalServerError {
		t.Fatalf("the password alone, offered twice = %d %s, want a refusal", again.Code, again.Body.String())
	}
	replay := call(t, router, http.MethodPost, challenge, `{"email":"`+email+`","code":"`+step+`"}`)
	if replay.Code == http.StatusOK || replay.Code == http.StatusCreated {
		t.Errorf("answering %s a second time with the step it already accepted = %d %s, want a refusal: "+
			"a TOTP step is spendable once — RFC 6238 §5.2 says a verifier that takes a code twice spent "+
			"it once, and the skew the standard allows is the window in which a code somebody else saw "+
			"would otherwise still be worth typing",
			challenge, replay.Code, replay.Body.String())
	}
	if sessionCookie(replay) != "" {
		t.Errorf("a replayed step set a session cookie at %s", challenge)
	}
	liveAfter, unusedAfter, eventsAfter := proofLedger(t, conn, ada)
	if liveAfter != liveBefore {
		t.Errorf("the replay left %d live sessions for %s, want the %d the first sign-in left: a refused "+
			"mutation writes no row", liveAfter, email, liveBefore)
	}
	if signedIn := countNamed(eventsAfter, contracts.EventLoggedIn); signedIn != signedInBefore {
		t.Errorf("the trail holds %d %s rows where it held %d: no command opened a session to describe",
			signedIn, contracts.EventLoggedIn, signedInBefore)
	}
	if unusedAfter != unusedBefore {
		t.Errorf("%d recovery codes are unspent after the replay is refused, want the %d held before it",
			unusedAfter, unusedBefore)
	}

	// And the account is not stuck: a credential this step never touched still
	// signs the person in, so the refusal above is about the step and not about
	// the door having had enough of this address.
	window := call(t, router, http.MethodPost, "/api/v1/auth/login",
		`{"email":"`+email+`","password":"`+authtest.Password+`"}`)
	if sessionCookie(window) != "" {
		t.Fatal("the password alone opened a session at an account that carries a factor")
	}
	recovered := call(t, router, http.MethodPost, challenge,
		`{"email":"`+email+`","code":"`+codes[len(codes)-1]+`"}`)
	if recovered.Code != http.StatusOK || sessionCookie(recovered) == "" {
		t.Fatalf("an unspent recovery code behind a refused sign-in = %d %s, cookie %q, want 200 and a session",
			recovered.Code, recovered.Body.String(), sessionCookie(recovered))
	}
	if liveNow, _, _ := proofLedger(t, conn, ada); liveNow != liveAfter+1 {
		t.Errorf("the sign-in that spent a recovery code left %d live sessions, want %d", liveNow, liveAfter+1)
	}
}
