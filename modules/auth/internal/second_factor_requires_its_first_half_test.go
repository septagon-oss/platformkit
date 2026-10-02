package internal_test

// What this file pins: the second half of a sign-in is only ever answerable by a
// caller who already offered the first one.
//
// `POST /challenge/verify` takes `{"email","code"}` and answers with a session
// cookie (`modules/auth/internal/factor_routes.go` → `handleVerifyFactor` →
// `Service.VerifySecondFactor`, `factors.go:231`). Between those two there is
// nothing that records — or requires — that this caller ever offered the first
// proof: no pending marker, no cookie on `Login`'s `ErrFactorRequired` refusal
// (`handler.go`'s `refusal` returns a problem and sets no cookie), no token in
// the 401 body for the caller to hand back. `contracts.Factors` calls this
// "the second half of a sign-in", and `factor_routes.go` says the door "opens
// the session the password had already earned". It does, whether or not a
// password was ever offered, and by anyone:
//
//	svc.VerifySecondFactor(ctx, tx, email, codes[1], nobody)   // factors_test.go:194
//
// is a first-party case that spends a *recovery code* as a stranger and expects
// a session. Recovery codes are 128 bits from crypto/rand, so nobody guesses
// one; they are also text a person keeps — a password manager, a file, a printed
// card, pasted into a chat to someone "helping". Every one of them is now, on
// its own, a sign-in: not a way into the second step for a person who has their
// password, but the whole account, with no password, from any machine, each code
// usable once so ten leaked codes are ten sign-ins. A TOTP code is the same door
// with a 30-second timer on it, which is the "read out the code your
// authenticator is showing" phishing that every 2FA design exists to make worth
// nothing.
//
// The module's own README states the rule the other way round — "A password
// proves something was typed. A second factor proves something was carried" —
// and its "Deliberately not here" list refuses to let *anything* stand as the
// first factor for a tenant: no passkey, no provider, no declaration. So the
// second factor is the one thing in this module that does not need the first,
// and `RotateRecoveryCodes`'s own comment — codes "are the substitute for a
// second factor and not a second one" — is the promise a leaked file breaks.
//
// What this case asserts is the outcome, not a cure: the leg may be bound to the
// first half by whatever state the delivery chooses (a marker on the refusal, a
// short-lived pending row, the surface the shell already holds), as long as a
// credential presented with no first half gets a refusal, opens no session,
// spends nothing and publishes no sign-in. Nothing here pins the *shape* of that
// refusal: the reachability guard below accepts any refusal the door may answer,
// and only rules out the answers that would mean the request never reached a
// credential check at all — no route, wrong method, rate-limited before the
// check. Every assertion is therefore true before a fix and after one, and none
// of them reads the wording or the status code the broken answer happens to use.

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/user"
)

func TestACredentialAnsweredWithoutItsFirstHalfSignsNobodyIn(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	const factorKey = "a factor key this deployment set"
	router, _, _, api := mountRecorded(t, conn, auth.OIDC{}, false, func(d *auth.Deps) {
		d.FactorKey = factorKey
	})

	// The address is read off the composition, so a cure that moves the leg to
	// another surface or path still has to answer this case.
	challenge := ""
	for _, m := range api.Mounted() {
		if m.Module == "auth" && m.Method == http.MethodPost && strings.Contains(m.Path, "challenge") {
			challenge = m.Path
		}
	}
	if challenge == "" {
		t.Fatal("the composition mounts no auth-challenge-verify operation: the second factor has no HTTP leg at all")
	}

	const email = "ada@acme.localhost"
	ada := person(t, conn, email, contracts.RoleAdmin)

	// Enrol a factor through the routes, so the codes this case spends are the
	// ones the module handed to a signed-in person.
	cookie := sessionCookie(call(t, router, http.MethodPost, "/api/v1/auth/login",
		`{"email":"`+email+`","password":"`+authtest.Password+`"}`))
	if cookie == "" {
		t.Fatal("the password sign-in that starts the enrolment set no session cookie")
	}
	begin := call(t, router, http.MethodPost, "/api/v1/auth/factors/totp/begin", "", withSession(cookie))
	if begin.Code != http.StatusOK {
		t.Fatalf("beginning an enrolment = %d %s, want 200", begin.Code, begin.Body.String())
	}
	var enrolment contracts.TOTPEnrolment
	if err := json.Unmarshal(begin.Body.Bytes(), &enrolment); err != nil {
		t.Fatalf("read the enrolment: %v (%s)", err, begin.Body.String())
	}
	finish := call(t, router, http.MethodPost, "/api/v1/auth/factors/totp/finish",
		`{"secret":"`+enrolment.Secret+`","code":"`+codeFor(t, enrolment.Secret, db.Now())+`"}`,
		withSession(cookie))
	if finish.Code != http.StatusCreated {
		t.Fatalf("finishing an enrolment = %d %s, want 201", finish.Code, finish.Body.String())
	}
	var issued struct {
		Codes []string `json:"codes"`
	}
	if err := json.Unmarshal(finish.Body.Bytes(), &issued); err != nil {
		t.Fatalf("read the recovery codes: %v (%s)", err, finish.Body.String())
	}
	if len(issued.Codes) == 0 {
		t.Fatalf("the enrolment issued no recovery codes: %s", finish.Body.String())
	}

	// Reachability, proven through answers that hold whatever this case finds: a
	// string that is neither a TOTP nor a recovery code, from a caller with no
	// cookie, is refused — and refused by something that looked at a credential,
	// which rules out "no such route", "wrong method" and "you have asked too
	// often, come back later" without naming the status a correct refusal wears.
	// A case that pinned one status here would be green only while the answer it
	// watches stayed as it is.
	probe := call(t, router, http.MethodPost, challenge, `{"email":"`+email+`","code":"not a code at all"}`)
	if probe.Code < http.StatusBadRequest || probe.Code >= http.StatusInternalServerError ||
		probe.Code == http.StatusNotFound || probe.Code == http.StatusMethodNotAllowed ||
		probe.Code == http.StatusRequestTimeout || probe.Code == http.StatusTooManyRequests {
		t.Fatalf("a caller with no first factor and a code that is no code at all gets %d at %s; "+
			"this case needs a refusal that came from a credential check, so it cannot assert anything "+
			"until the door answers that way (%s)", probe.Code, challenge, probe.Body.String())
	}
	if sessionCookie(probe) != "" {
		t.Fatalf("a refusal at %s set a session cookie: no answer this door gives that is not a sign-in "+
			"may open a session", challenge)
	}

	// Everything this case reads back, before the request that may spend any of it.
	liveBefore, unusedBefore, eventsBefore := factorLedger(t, conn, ada)
	if liveBefore != 1 {
		t.Fatalf("%d sessions are live for %s before the challenge, want the one the enrolment signed in with",
			liveBefore, email)
	}
	if unusedBefore != len(issued.Codes) {
		t.Fatalf("%d unused recovery codes are held, want the %d the enrolment handed out",
			unusedBefore, len(issued.Codes))
	}

	// The claim. A caller who never offered a password, holding one of this
	// person's own recovery codes, is refused — and the refusal spends nothing.
	answer := call(t, router, http.MethodPost, challenge,
		`{"email":"`+email+`","code":"`+issued.Codes[0]+`"}`)
	if answer.Code == http.StatusOK || answer.Code == http.StatusCreated {
		t.Errorf("answering %s with a recovery code and no first factor = %d %s, want a refusal: "+
			"a code that was never offered is the whole of what this caller proved, and a recovery code "+
			"stands in for the second factor, it is not a credential that replaces the first one",
			challenge, answer.Code, answer.Body.String())
	}
	if got := sessionCookie(answer); got != "" {
		t.Errorf("a recovery code answered with no first factor set a session cookie at %s: "+
			"a leaked code is now a password, usable from any machine, that no policy on this account can refuse",
			challenge)
	}
	liveAfter, unusedAfter, eventsAfter := factorLedger(t, conn, ada)
	if liveAfter != liveBefore {
		t.Errorf("the leg opened %d live session(s) for %s on a recovery code alone (was %d): "+
			"a refused mutation writes no row, and this one was refused",
			liveAfter-liveBefore, email, liveBefore)
	}
	if unusedAfter != unusedBefore {
		t.Errorf("%d recovery codes are unused after the refusal, want the %d before it: "+
			"a refusal that spends a code both loses the code and records a use of a sign-in that did not happen",
			unusedAfter, unusedBefore)
	}
	if signedIn := countNamed(eventsAfter, contracts.EventLoggedIn); signedIn != countNamed(eventsBefore, contracts.EventLoggedIn) {
		t.Errorf("the outbox holds %d %s rows where it held %d: a caller who was refused was not signed in, "+
			"and an event that describes a session no command opened is a trail that cannot be read",
			signedIn, contracts.EventLoggedIn, countNamed(eventsBefore, contracts.EventLoggedIn))
	}
	if spent := countNamed(eventsAfter, contracts.EventRecoveryCodeUsed); spent != countNamed(eventsBefore, contracts.EventRecoveryCodeUsed) {
		t.Errorf("the outbox holds %d %s rows where it held %d: the refusal spent a code and said it did",
			spent, contracts.EventRecoveryCodeUsed, countNamed(eventsBefore, contracts.EventRecoveryCodeUsed))
	}

	// The same door, asked a TOTP the same way: a code from the current step, from
	// a caller who never offered the password, is not a sign-in either.
	totp := call(t, router, http.MethodPost, challenge,
		`{"email":"`+email+`","code":"`+codeFor(t, enrolment.Secret, db.Now())+`"}`)
	if totp.Code == http.StatusOK || totp.Code == http.StatusCreated {
		t.Errorf("answering %s with a current TOTP and no first factor = %d %s, want a refusal: "+
			"the six digits are the thing a person is asked to read out to whoever phished them, "+
			"and here that is enough to be signed in",
			challenge, totp.Code, totp.Body.String())
	}
	if got := sessionCookie(totp); got != "" {
		t.Errorf("a current TOTP answered with no first factor set a session cookie at %s", challenge)
	}
	if liveNow, _, _ := factorLedger(t, conn, ada); liveNow != liveBefore {
		t.Errorf("%d sessions are live for %s after the TOTP refusal, want the %d held before it",
			liveNow, email, liveBefore)
	}
}

// factorLedger reads the three things a refused sign-in must leave alone: the
// sessions this person holds, the recovery codes nobody has spent, and every
// auth event this tenant has published.
func factorLedger(t *testing.T, conn *db.Conn, person uuid.UUID) (live, unused int, events []string) {
	t.Helper()
	err := db.Run(httpx.WithConn(tenancy.WithTenant(t.Context(), acme), conn), conn,
		func(_ context.Context, tx db.Tx[db.Tenant]) error {
			held, err := liveFactorSessions(t, tx, person)
			if err != nil {
				return err
			}
			live = held
			var spare int64
			if err := tx.DB().Table("recovery_codes").Where("used_at IS NULL").Count(&spare).Error; err != nil {
				return err
			}
			unused = int(spare)
			events = outbox(t, tx)
			return nil
		})
	if err != nil {
		t.Fatalf("read the factor ledger: %v", err)
	}
	return live, unused, events
}

// countNamed is how many of these outbox names are one event.
func countNamed(names []string, want string) int {
	n := 0
	for _, name := range names {
		if name == want {
			n++
		}
	}
	return n
}
