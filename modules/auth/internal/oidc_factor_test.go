package internal_test

// The refusal at the federated door is a step, not a lockout.
//
// The defect was that `Service.Open` — the single sign-on callback's only
// caller — opened a session for a person whose account answers with a second
// factor, while `Service.Login` refuses the same person. `Open` now asks the
// account the same question, and `TestASingleSignOnLegDoesNotWalkPastAPersonsOwnSecondFactor`
// in `second_factor_gates_every_door_test.go` is the case that says so: it proves nothing
// is signed in at the federated door, and that a colleague who enrolled nothing still is.
//
// What that case does not say is the half this file owes: a person who holds a
// factor and arrives at
// the provider is now refused something they were not refused before, so the
// refusal has to be the same second half of a sign-in the password leg already
// hands over, and it has to work. This case walks it end to end over the routes —
// the refused callback, the challenge answered with the person's own code, and the
// session that comes out — and it holds the refusal to the rule every refused
// command on this module follows: nothing written, nothing published, no stale
// row. A fix that locked the person out of their own account would pass the sibling
// case and fail this one.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/user"
)

func TestAFederatedSignInRefusedForItsSecondHalfFinishesAtTheChallenge(t *testing.T) {
	issuer := authtest.NewIssuer(t)
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	router, _, _ := mountConfigured(t, conn, auth.OIDC{
		Issuer: issuer.URL, ClientID: "platformkit", ClientSecret: "secret",
		RedirectPath: "/api/v1/auth/oidc/callback",
	}, false, func(d *auth.Deps) { d.FactorKey = "a factor key this deployment set" })

	const ada = "ada@acme.localhost"
	personID := person(t, conn, ada, contracts.RoleMember)

	// leg is the authorization-code round trip a browser makes: /oidc/start, the
	// provider's code, /oidc/callback, with the state cookie in between.
	leg := func(t *testing.T, code string) *httptest.ResponseRecorder {
		t.Helper()
		res := call(t, router, http.MethodGet, "/api/v1/auth/oidc/start", "")
		if res.Code != http.StatusSeeOther {
			t.Fatalf("oidc/start = %d %s, want 303", res.Code, res.Body.String())
		}
		to, err := url.Parse(res.Header().Get("Location"))
		if err != nil {
			t.Fatalf("the redirect is not a URL: %v", err)
		}
		var state string
		for _, c := range (&http.Response{Header: res.Header()}).Cookies() {
			if c.Name == "platformkit_oidc" {
				state = c.Value
			}
		}
		if state == "" {
			t.Fatal("start set no state cookie")
		}
		issuer.Issue(code, ada, true, "platformkit", to.Query().Get("nonce"))
		return call(t, router, http.MethodGet,
			"/api/v1/auth/oidc/callback?code="+url.QueryEscape(code)+
				"&state="+url.QueryEscape(to.Query().Get("state")),
			"", func(r *http.Request) { r.AddCookie(&http.Cookie{Name: "platformkit_oidc", Value: state}) })
	}

	// The person signs in over the leg while the password alone is enough, and
	// enrols a factor with that session. The factor has to arrive by the routes,
	// because the account's state is the thing being decided about.
	first := sessionCookie(leg(t, "code-before"))
	if first == "" {
		t.Fatal("the federated leg signed nobody in before a factor existed, so nothing below tests the factor")
	}
	begin := call(t, router, http.MethodPost, "/api/v1/auth/factors/totp/begin", "", withSession(first))
	if begin.Code != http.StatusOK {
		t.Fatalf("beginning an enrolment = %d %s, want 200", begin.Code, begin.Body.String())
	}
	var enrolment contracts.TOTPEnrolment
	if err := json.Unmarshal(begin.Body.Bytes(), &enrolment); err != nil || enrolment.Secret == "" {
		t.Fatalf("the enrolment showed no secret: %v (%s)", err, begin.Body.String())
	}
	finish := call(t, router, http.MethodPost, "/api/v1/auth/factors/totp/finish",
		`{"secret":"`+enrolment.Secret+`","code":"`+codeFor(t, enrolment.Secret, db.Now())+`"}`,
		withSession(first))
	if finish.Code != http.StatusCreated {
		t.Fatalf("finishing an enrolment = %d %s, want 201", finish.Code, finish.Body.String())
	}

	// The refused leg, held to the rule a refused mutation follows everywhere in
	// this module: the answer is a step, and behind it no row, no event.
	held := func(t *testing.T) (int, int) {
		t.Helper()
		var sessions, published int
		err := db.Run(tenancy.WithTenant(t.Context(), acme), conn,
			func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				live, err := liveFactorSessions(t, tx, personID)
				if err != nil {
					return err
				}
				sessions, published = live, len(outbox(t, tx))
				return nil
			})
		if err != nil {
			t.Fatalf("read what the refused sign-in left: %v", err)
		}
		return sessions, published
	}
	liveBefore, eventsBefore := held(t)
	refused := leg(t, "code-after")
	if refused.Code != http.StatusUnauthorized {
		t.Errorf("the federated leg for a person holding a factor = %d %s, want 401",
			refused.Code, refused.Body.String())
	}
	if !strings.Contains(refused.Body.String(), "second factor") {
		t.Errorf("the refusal says nothing about what is missing: %s", refused.Body.String())
	}
	if sessionCookie(refused) != "" {
		t.Error("the refused federated leg set a session cookie")
	}
	if live, events := held(t); live != liveBefore || events != eventsBefore {
		t.Errorf("the refused leg opened %d sessions and published %d events, want 0 of each: a sign-in "+
			"refused for its second half is neither a success (auth.logged_in describes a row that does not "+
			"exist) nor an attack (the provider did confirm the address)", live-liveBefore, events-eventsBefore)
	}

	// And the step is walkable. The person who was refused above answers with the
	// code their own device is showing, on the route the refusal names, and gets
	// the session the provider had already earned — which is the difference
	// between the door asking for two things and the door being shut.
	challenge := call(t, router, http.MethodPost, "/api/v1/auth/challenge/verify",
		`{"email":"`+ada+`","code":"`+codeFor(t, enrolment.Secret, db.Now())+`"}`)
	if challenge.Code != http.StatusOK {
		t.Fatalf("answering the challenge = %d %s, want 200", challenge.Code, challenge.Body.String())
	}
	finished := sessionCookie(challenge)
	if finished == "" {
		t.Fatal("the challenge set no session cookie")
	}
	if me := call(t, router, http.MethodGet, "/api/v1/auth/me", "", withSession(finished)); me.Code != http.StatusOK ||
		!strings.Contains(me.Body.String(), ada) {
		t.Errorf("the session the challenge opened = %d %s at /auth/me, want 200 for %s",
			me.Code, me.Body.String(), ada)
	}

	// The refused callback wrote nothing, so the only sessions this person has are
	// the two legs that actually completed: the first one, and the challenge.
	if live, _ := held(t); live != liveBefore+1 {
		t.Errorf("this person holds %d live sessions after the walk, want %d: one from the leg that opened "+
			"one, one from the challenge, and none from the refusal between them", live, liveBefore+1)
	}
}
