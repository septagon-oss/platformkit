package internal_test

// Review 6 (decision 0039: HIGHs only) over the door this branch did not write a
// factor check into.
//
// The branch shipped two things in one delivery: an issuer per tenant (`8b29ea0`)
// and a second factor (`af665cb`). Each is tested against itself, and the case
// below is the one place they meet.
//
// `Service.Login` decides about the *account*, in its own words: "Whether that is
// enough is a fact about the account rather than about this request: a person who
// enrolled a second factor is not signed in by the first half of their own
// sign-in, and opening a session here and taking it away afterwards would be a
// window in which a stolen password was a stolen account for as long as the code
// took to type." It then returns ErrFactorRequired and writes no row (service.go
// :154-159).
//
// `Service.Open` is the other door that writes a session row — the single caller
// is the OIDC callback, oidc.go:337 — and it asks only whether the person is
// active. There is no factor question on that side, and no tenant field that
// could answer one: `tenants` carries an issuer, a client id, a secret reference,
// a registration mode and roles, and nothing that says "this tenant's identity
// provider is trusted to have asked for a second factor". `contracts.OIDCProvider`
// carries no such field either.
//
// So the sentence above is true of one address in this module and false of the
// other, and the two are reachable from the same browser at the same tenant: the
// control plane writes the issuer (`POST /tenants/{id}/oidc`, which the reference
// application's own composition test drives), and the person enrols the factor at
// `/api/v1/auth/factors/totp/finish`. The person's own state says a second thing
// is required; the session the federated leg opens does not care.
//
// Both fixes are honest and neither is this review's to choose: the federated leg
// asks the question the account already answers (the assertion below is exactly
// that, and a redirect to the challenge leg satisfies it), or the exemption
// becomes a declaration the tenant makes and the module refuses to open a session
// without it — which is the "where the tenant allows" of the brief's item 2, still
// unbuilt. What is not honest is the present silence: no row, no field, no README
// sentence, and no test says that a locally-enrolled second factor is a
// password-leg rule only.
//
// Every assertion is about what a working door produces — a cookie that opens
// `/auth/me`, or the absence of one — and the factor-less colleague is checked
// last, so the case cannot be satisfied by refusing single sign-on to everybody,
// nor by the factor leg refusing to enrol.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/user"
)

func TestASingleSignOnLegDoesNotWalkPastAPersonsOwnSecondFactor(t *testing.T) {
	issuer := authtest.NewIssuer(t)
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	router, _, _ := mountConfigured(t, conn, auth.OIDC{
		Issuer: issuer.URL, ClientID: "platformkit", ClientSecret: "secret",
		RedirectPath: "/api/v1/auth/oidc/callback",
	}, false, func(d *auth.Deps) { d.FactorKey = "a factor key this deployment set" })

	const ada = "ada@acme.localhost"
	const colleague = "rob@acme.localhost"
	person(t, conn, ada, contracts.RoleAdmin)
	person(t, conn, colleague, contracts.RoleMember)

	// signThrough is the authorization-code round trip, both legs, through the
	// routes: /oidc/start, the fake provider's code, /oidc/callback.
	signThrough := func(t *testing.T, email, code string) *httptest.ResponseRecorder {
		t.Helper()
		res := call(t, router, http.MethodGet, "/api/v1/auth/oidc/start", "")
		if res.Code != http.StatusSeeOther {
			t.Fatalf("oidc/start = %d %s, want 303", res.Code, res.Body.String())
		}
		to, err := url.Parse(res.Header().Get("Location"))
		if err != nil {
			t.Fatalf("the redirect is not a URL: %v", err)
		}
		nonce := to.Query().Get("nonce")
		var stateCookie string
		for _, c := range (&http.Response{Header: res.Header()}).Cookies() {
			if c.Name == "platformkit_oidc" {
				stateCookie = c.Value
			}
		}
		if stateCookie == "" {
			t.Fatal("start set no state cookie")
		}
		issuer.Issue(code, email, true, "platformkit", nonce)
		return call(t, router, http.MethodGet,
			"/api/v1/auth/oidc/callback?code="+url.QueryEscape(code)+"&state="+url.QueryEscape(to.Query().Get("state")),
			"", func(r *http.Request) { r.AddCookie(&http.Cookie{Name: "platformkit_oidc", Value: stateCookie}) })
	}

	// First: the federated leg works, before anybody holds a factor. Without this
	// half the refusal below could be bought by breaking single sign-on.
	before := signThrough(t, ada, "code-ada-before")
	if sessionCookie(before) == "" {
		t.Fatalf("the federated leg set no session for a person holding no factor = %d %s, want one: "+
			"this case is about the factor, not about whether single sign-on works",
			before.Code, before.Body.String())
	}

	// Ada enrols a second factor over her own two routes, with the session that
	// leg just opened.
	begin := call(t, router, http.MethodPost, "/api/v1/auth/factors/totp/begin", "", withSession(sessionCookie(before)))
	if begin.Code != http.StatusOK {
		t.Fatalf("beginning an enrolment = %d %s, want 200", begin.Code, begin.Body.String())
	}
	var enrolment contracts.TOTPEnrolment
	if err := json.Unmarshal(begin.Body.Bytes(), &enrolment); err != nil || enrolment.Secret == "" {
		t.Fatalf("the enrolment showed no secret: %v (%s)", err, begin.Body.String())
	}
	finish := call(t, router, http.MethodPost, "/api/v1/auth/factors/totp/finish",
		`{"secret":"`+enrolment.Secret+`","code":"`+codeFor(t, enrolment.Secret, db.Now())+`"}`,
		withSession(sessionCookie(before)))
	if finish.Code != http.StatusCreated {
		t.Fatalf("finishing an enrolment = %d %s, want 201", finish.Code, finish.Body.String())
	}

	// The account now holds a factor, and the password leg says so by opening
	// nothing. Checked as a missing cookie rather than as a status or a sentence.
	halted := call(t, router, http.MethodPost, "/api/v1/auth/login",
		`{"email":"`+ada+`","password":"`+authtest.Password+`"}`)
	if sessionCookie(halted) != "" {
		t.Fatalf("the password leg opened a session for a person who holds a factor = %d, want none", halted.Code)
	}

	// The same account at the tenant's other door: an issuer the control plane
	// wrote, a code the provider minted, an address verified against it.
	after := signThrough(t, ada, "code-ada-after")
	if live := sessionCookie(after); live != "" {
		me := call(t, router, http.MethodGet, "/api/v1/auth/me", "", withSession(live))
		t.Errorf("the federated leg opened a live session for %s, who holds a second factor: the callback = %d "+
			"and the cookie it set answers /auth/me with %d %s. Service.Open (service.go:171) asks only whether "+
			"this person is active, while Service.Login (:154) is the one that asks the account whether a second "+
			"thing is required — so a person's own enrolled factor stops the password and not the tenant's "+
			"identity provider, and nothing in a row, a field or this module's README says that is the rule "+
			"(the brief's item 2 asks for \"where the tenant allows\", and no tenant says anything here).",
			ada, after.Code, me.Code, me.Body.String())
	}

	// And last, the guard that keeps the refusal above from being bought by
	// breaking single sign-on: the colleague who enrolled nothing is still signed
	// in by the same leg, at the same tenant, in the same process.
	free := signThrough(t, colleague, "code-rob-after")
	colleagueSession := sessionCookie(free)
	if colleagueSession == "" {
		t.Fatalf("the federated leg set no session for the person who holds no factor = %d %s, want one",
			free.Code, free.Body.String())
	}
	if me := call(t, router, http.MethodGet, "/api/v1/auth/me", "", withSession(colleagueSession)); me.Code != http.StatusOK {
		t.Errorf("the colleague's federated session = %d %s at /auth/me, want 200", me.Code, me.Body.String())
	}
}
