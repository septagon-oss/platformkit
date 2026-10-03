package internal_test

// review 4 (decision 0039: HIGHs only). The second factor's sign-in leg, tried at
// the address the module serves it at.
//
// Nothing in this repository has ever sent a request to
// /api/v1/public/auth/challenge/verify. The only two places that name it are its
// own httpx.Register call and one row of surface_test.go's table:
//
//	$ grep -rn "challenge/verify" --include=*.go --include=*.ts .
//	  modules/auth/internal/surface_test.go:76: {"public", "POST", "/api/v1/public/auth/challenge/verify", "public"},
//	  modules/auth/internal/factor_routes.go:95:  Path: "/challenge/verify",
//
// TestAFactorEnrolsAndIsWhatSignsThatPersonIn walks the whole sequence — enrol,
// halt, sign in with a code, spend a recovery code — through svc.BeginTOTP,
// svc.FinishTOTP and svc.VerifySecondFactor, in one transaction, with no router in
// sight. That is why "the API path works and is tested" (modules/auth/README.md) is
// a claim about the service and not about the route.
//
// The route matters because the surface decides what its response may do. The
// challenge mounts on surfaces.Public — the anonymous face, /api/v1/public/… — and
// kit/httpx keeps that surface's promise at the writer: buffer.withholdCookies
// takes every Set-Cookie off a response whose SurfaceOf is SurfacePublic
// (kit/httpx/buffer.go:116), and respond turns a withheld cookie into a 500 naming
// the route (kit/httpx/respond.go:57-63, CodePublicSetsACookie). The two cookie
// routes that preceded this one mount on surfaces.App with httpx.Public()
// authorisation instead — /login is handler.go:35's `httpx.Register(app, …)` — and
// factor_routes.go's own comment says the challenge does the same thing: "mounts on
// the workspace surface, which is how /login and /password/reset already work …
// It cannot live on the anonymous surface, which sets no cookie and is skipped by
// Authenticate by design — a challenge answered there could sign nobody in". The
// code mounts it on the surface that comment calls the anonymous one.
//
// So this case does what the brief's Done-when asks, one credential down from a
// passkey: enrol through the routes, be halted by the password, answer the
// challenge at its address, and use the session that answer opens. Every assertion
// is about what a working leg produces — a 200, a cookie, a request that cookie
// answers — so the case cannot depend on the shape of the defect it is there to
// catch.

import (
	"encoding/json"
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

func TestASecondFactorSignsThatPersonInOverItsOwnRoute(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	const factorKey = "a factor key this deployment set"
	router, _, _, api := mountRecorded(t, conn, auth.OIDC{}, false, func(d *auth.Deps) {
		d.FactorKey = factorKey
	})
	// The address is read off the composition rather than written here, because the
	// case is about what the challenge leg does, not about where it lives: a fix
	// that moves the leg to another surface must satisfy this case, and a fix that
	// leaves it unable to hand over a session must not.
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
	person(t, conn, email, contracts.RoleAdmin)

	// A password signs this person in while no factor exists.
	res := call(t, router, http.MethodPost, "/api/v1/auth/login",
		`{"email":"`+email+`","password":"`+authtest.Password+`"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("password sign-in before any factor = %d %s, want 200", res.Code, res.Body.String())
	}
	cookie := sessionCookie(res)
	if cookie == "" {
		t.Fatal("the sign-in set no session cookie")
	}

	// Enrolment, through the two routes rather than the two methods: the secret is
	// shown once, and a code the device produced is what makes the factor exist.
	begin := call(t, router, http.MethodPost, "/api/v1/auth/factors/totp/begin", "", withSession(cookie))
	if begin.Code != http.StatusOK {
		t.Fatalf("beginning an enrolment = %d %s, want 200", begin.Code, begin.Body.String())
	}
	var enrolment contracts.TOTPEnrolment
	if err := json.Unmarshal(begin.Body.Bytes(), &enrolment); err != nil {
		t.Fatalf("read the enrolment: %v (%s)", err, begin.Body.String())
	}
	if enrolment.Secret == "" {
		t.Fatalf("the enrolment showed no secret: %s", begin.Body.String())
	}
	code := codeFor(t, enrolment.Secret, db.Now())
	finish := call(t, router, http.MethodPost, "/api/v1/auth/factors/totp/finish",
		`{"secret":"`+enrolment.Secret+`","code":"`+code+`"}`, withSession(cookie))
	if finish.Code != http.StatusCreated {
		t.Fatalf("finishing an enrolment = %d %s, want 201", finish.Code, finish.Body.String())
	}

	// The password alone is now refused, and refused in a way that keeps the person
	// on the page: no session, and the answer says one more thing is missing.
	halted := call(t, router, http.MethodPost, "/api/v1/auth/login",
		`{"email":"`+email+`","password":"`+authtest.Password+`"}`)
	if halted.Code != http.StatusUnauthorized {
		t.Fatalf("the password alone after enrolling a factor = %d %s, want 401", halted.Code, halted.Body.String())
	}
	if sessionCookie(halted) != "" {
		t.Fatal("the password alone opened a session after a factor was enrolled")
	}

	// The challenge, at the address the module registers it at, with a fresh code.
	// This is the assertion the whole capability comes down to: a right answer to
	// the second factor is a signed-in caller, with a cookie, at an HTTP address.
	answered := call(t, router, http.MethodPost, challenge,
		`{"email":"`+email+`","code":"`+codeFor(t, enrolment.Secret, db.Now())+`"}`)
	if answered.Code != http.StatusOK {
		t.Errorf("answering the second factor at %s with the current code = %d %s, want 200",
			challenge, answered.Code, answered.Body.String())
	}
	signedIn := sessionCookie(answered)
	if signedIn == "" {
		t.Errorf("the answered challenge at %s set no session cookie: nobody was signed in by a correct code",
			challenge)
	} else {
		// The session is a caller, not a body: the same identity a password login
		// answers with, and it opens a route that needs one.
		me := call(t, router, http.MethodGet, "/api/v1/auth/me", "", withSession(signedIn))
		if me.Code != http.StatusOK {
			t.Errorf("the session an answered challenge = %d %s at /api/v1/auth/me, want 200",
				me.Code, me.Body.String())
		}
	}
}
