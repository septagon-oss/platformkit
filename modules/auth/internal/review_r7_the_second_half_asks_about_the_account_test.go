package internal_test

// The two halves of the cure for review 6's HIGH, pinned from the outside.
//
// `3ff61ad` closed the door `Service.Open` left open: a person whose account
// answers with a second factor is no longer signed in by a provider that proved
// only the first half. Review 6's own file pins that refusal at a live account,
// and `oidc_factor_test.go` pins that the refused person can still finish at the
// challenge. Two consequences of the same four lines are pinned by neither, and
// both are the way the cure could quietly rot without a case going red:
//
//  1. The refusal is about an open account's owner, not about a closed one. The
//     four lines sit after the active check on purpose — the commit says so: "a
//     closed account still gets the one answer it got before and the oracle does
//     not widen". Nobody ran a person who holds an enrolled factor and whose
//     account this tenant closed. If the order flips, or if the factor question
//     moves into the callback in front of `ConfirmAddress`, a closed account
//     starts learning — from a public address, over its own provider — that a
//     second factor is enrolled for it, which is one fact more than that door ever
//     gave. The case reaches that through a status code and a row count, never
//     through a sentence, and ends with a live colleague on the same leg, so a
//     door shut to everybody satisfies nothing either.
//
//  2. The question is asked of an account, in a tenant, per request. This brief
//     shipped an issuer per tenant beside the factor, and
//     `TestTwoTenantsSignInAtTwoIssuersInOneProcess` proves the *providers*
//     resolve per host — but it enrols no factor at either tenant. A refusal that
//     spread past the account (a process-wide flag, a `Deps` bool, a factor
//     counted on a connection that never resolved a tenant) would sign nobody in
//     anywhere and leave that case green. This one enrols at one tenant and
//     requires the other tenant's own person to walk the same leg into a session,
//     twice, on either side of the refusal.

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
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
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// federate walks the browser's leg — /oidc/start, the provider's code,
// /oidc/callback — at whichever host the caller names, and returns the
// callback's answer. It goes through the two-issuer file's own `start` and
// `callback` readers rather than a third copy of how to hold a state cookie.
func federate(t *testing.T, router chi.Router, issuer *authtest.Issuer, at, email, code string) *httptest.ResponseRecorder {
	t.Helper()
	_, state, cookie, _ := start(t, router, at)
	issuer.Issue(code, email, true, "platformkit", nonce(cookie))
	return callback(t, router, at, code, state, cookie)
}

// enrolFactor gives a person a TOTP over their own two routes, with the session
// the caller hands them. It arrives by the routes rather than by a row write
// because what is under test is what an enrolled account does at a door.
func enrolFactor(t *testing.T, router chi.Router, session string) string {
	t.Helper()
	begin := call(t, router, http.MethodPost, "/api/v1/auth/factors/totp/begin", "", withSession(session))
	if begin.Code != http.StatusOK {
		t.Fatalf("beginning an enrolment = %d %s, want 200", begin.Code, begin.Body.String())
	}
	var enrolment contracts.TOTPEnrolment
	if err := json.Unmarshal(begin.Body.Bytes(), &enrolment); err != nil || enrolment.Secret == "" {
		t.Fatalf("the enrolment showed no secret: %v (%s)", err, begin.Body.String())
	}
	finish := call(t, router, http.MethodPost, "/api/v1/auth/factors/totp/finish",
		`{"secret":"`+enrolment.Secret+`","code":"`+codeFor(t, enrolment.Secret, db.Now())+`"}`,
		withSession(session))
	if finish.Code != http.StatusCreated {
		t.Fatalf("finishing an enrolment = %d %s, want 201", finish.Code, finish.Body.String())
	}
	return enrolment.Secret
}

// liveSessionRows counts what Identify would still accept for this person in this
// tenant. A status code is what a refusal can get wrong on purpose; a row cannot.
func liveSessionRows(t *testing.T, conn *db.Conn, tenant tenancy.Tenant, userID uuid.UUID) int {
	t.Helper()
	var count int64
	err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn,
		func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			held, err := liveFactorSessions(t, tx, userID)
			count = int64(held)
			return err
		})
	if err != nil {
		t.Fatalf("count the live sessions of %s: %v", userID, err)
	}
	return int(count)
}

// personAt is the http_test.go helper with the tenant named, which the two-tenant
// case needs: `person` writes at acme and that is the only tenant it knows.
func personAt(t *testing.T, conn *db.Conn, tenant tenancy.Tenant, email string, roles ...string) uuid.UUID {
	t.Helper()
	users := realUsers()
	var id uuid.UUID
	err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn,
		func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			u, err := users.Invite(ctx, tx, email, "")
			if err != nil {
				return err
			}
			if err := users.SetPassword(ctx, tx, u.ID, authtest.Password); err != nil {
				return err
			}
			if len(roles) > 0 {
				if _, err := users.SetRoles(ctx, tx, u.ID, roles); err != nil {
					return err
				}
			}
			id = u.ID
			return nil
		})
	if err != nil {
		t.Fatalf("create %s at %s: %v", email, tenant.Slug, err)
	}
	return id
}

// closeAccount is the tenant removing access, through the user module's own
// command rather than an UPDATE: `Deactivate` is what the generated screen calls,
// and it deliberately leaves the person's sessions for whoever owns them.
func closeAccount(t *testing.T, conn *db.Conn, id uuid.UUID) {
	t.Helper()
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn,
		func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			_, err := realUsers().Deactivate(ctx, tx, id)
			return err
		})
	if err != nil {
		t.Fatalf("closing the account: %v", err)
	}
}

// mountTwoWithFactors is mountTwo with one line added and nothing else changed:
// the factor key. `mountTwo` takes no look at Deps, and a factor cannot be
// enrolled without the key its secrets are sealed with, so the per-tenant claim
// below needs a mount that is otherwise the same composition.
func mountTwoWithFactors(t *testing.T, providers contracts.OIDCProviders, secrets contracts.Secrets) (chi.Router, *db.Conn) {
	t.Helper()
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	users, userModule := user.Module(user.Deps{Administration: &usercontracts.AdministrationFunc{Ask: auth.AdministeringRoles}})
	svc, authModule := auth.Module(auth.Deps{
		Users: users, Notify: &authtest.Notices{}, Mailer: &authtest.Mailbox{},
		Hosts: authtest.Host(host), OIDCProviders: providers, Secrets: secrets,
		PublicHost: host, FactorKey: "a factor key this deployment set",
	})
	seed(t, conn, acme)
	seed(t, conn, globex)

	api, router := httpx.New(httpx.Options{
		PublicHost: host, Tenants: twoSites{}, Conn: conn,
		Authorize: svc, Authenticate: svc.Authenticate, Log: slog.New(slog.DiscardHandler),
	})
	api.Declare([]tenancy.Grant{{Permission: contracts.PermissionRoleManage}})
	authModule.Routes(surfacesOf(api))
	userModule.Routes(api.Surfaces("user"))
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the mounted routes do not declare themselves: %v", err)
	}
	return router, conn
}

// TestAClosedAccountIsRefusedByTheDoorThatWasAlreadyRefusingIt walks one person
// from a live session with an enrolled factor to an account this tenant closed,
// and asks the federated door what it says at each step.
//
// While the account is open the leg is refused for its second half, which is
// review 6's finding and its case. Once the tenant has closed the account the leg
// must be refused by the refusal that predates this branch — 403, the account, not
// the credential — and the answer has to stay exactly that: the same status it
// gave a closed account with no factor, no session row, and no sentence about
// second factors, because a person whose access this tenant removed gains nothing
// from learning which half a door thinks is missing. The live colleague last is
// the guard against the cheap way out.
func TestAClosedAccountIsRefusedByTheDoorThatWasAlreadyRefusingIt(t *testing.T) {
	issuer := authtest.NewIssuer(t)
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	router, _, _ := mountConfigured(t, conn, auth.OIDC{
		Issuer: issuer.URL, ClientID: "platformkit", ClientSecret: "secret",
		RedirectPath: "/api/v1/auth/oidc/callback",
	}, false, func(d *auth.Deps) { d.FactorKey = "a factor key this deployment set" })

	const ada = "ada@acme.localhost"
	const colleague = "grace@acme.localhost"
	adaID := person(t, conn, ada, contracts.RoleMember)
	person(t, conn, colleague, contracts.RoleMember)

	// The factor arrives over the leg the person is actually signed in by, so the
	// account state is the one a person can reach and not one a test invented.
	first := sessionCookie(federate(t, router, issuer, host, ada, "ada-open"))
	if first == "" {
		t.Fatal("the federated leg signed nobody in before a factor existed, so nothing below tests a factor")
	}
	enrolFactor(t, router, first)

	open := liveSessionRows(t, conn, acme, adaID)
	held := federate(t, router, issuer, host, ada, "ada-held")
	if held.Code != http.StatusUnauthorized {
		// Review 6's own case owns this claim; here it is the reason the ordering
		// below means anything: the door did ask the account while it was open.
		t.Errorf("the federated leg for an open account holding a factor = %d %s, want 401",
			held.Code, held.Body.String())
	}
	if sessionCookie(held) != "" {
		t.Error("the leg refused for its second half set a session cookie")
	}

	// The tenant removes the account's access. `Deactivate` leaves the sessions
	// that already exist, so the count is measured again rather than assumed zero.
	closeAccount(t, conn, adaID)
	closed := liveSessionRows(t, conn, acme, adaID)
	if closed != open {
		t.Fatalf("closing the account moved its sessions (%d then %d); this case counts what a refused "+
			"leg adds, and it needs the rows that were already there", open, closed)
	}

	refused := federate(t, router, issuer, host, ada, "ada-closed")
	if refused.Code != http.StatusForbidden {
		t.Errorf("a closed account at its own federated door = %d %s, want the 403 the door gave before "+
			"this branch wrote a second factor: the account check owns this answer and the factor question "+
			"must not overtake it", refused.Code, refused.Body.String())
	}
	if strings.Contains(strings.ToLower(refused.Body.String()), "second factor") {
		t.Errorf("a closed account was told about a second factor at a public address: %s — the door that "+
			"cannot open an account has no reason to say which half it thinks is missing, and a person with "+
			"a code on their phone can read that sentence from outside", refused.Body.String())
	}
	if sessionCookie(refused) != "" {
		t.Error("the closed account's refused leg set a session cookie")
	}
	if now := liveSessionRows(t, conn, acme, adaID); now != closed {
		t.Errorf("the refused leg at a closed account added %d sessions: a mutation refused at the door "+
			"writes no row", now-closed)
	}

	// And the door is not shut: a colleague of the closed person, at the same
	// tenant, over the same issuer, in the same process, still gets a session.
	session := sessionCookie(federate(t, router, issuer, host, colleague, "grace-open"))
	if session == "" {
		t.Fatalf("the leg set no session for a person holding no factor (%d %s); a door that refuses "+
			"everybody would satisfy every assertion above", refused.Code, refused.Body.String())
	}
	if me := call(t, router, http.MethodGet, "/api/v1/auth/me", "", withSession(session)); me.Code != http.StatusOK ||
		!strings.Contains(me.Body.String(), colleague) {
		t.Errorf("the colleague's session = %d %s at /auth/me, want 200 for %s", me.Code, me.Body.String(), colleague)
	}
}

// TestTheSecondHalfIsAskedOfAnAccountAndNotOfTheProcess puts the factor in one
// tenant and a second, working issuer in the other, and requires three things at
// once: the person who holds a factor is refused at their own door, their
// factor-less colleague at that same tenant is signed in, and a person at the
// other tenant — whose factor does not exist, in a tenant that never enrolled one
// — walks its own leg into a session on both sides of the refusal.
//
// This is the pillar contract's second line for this delivery: every choice the
// pillar makes per tenant is resolved per request. A refusal that leaked out of
// the account into the process fails here and stays green in every case that
// enrols nothing.
func TestTheSecondHalfIsAskedOfAnAccountAndNotOfTheProcess(t *testing.T) {
	acmeIDP := authtest.NewIssuer(t)
	globexIDP := authtest.NewIssuer(t)
	router, conn := mountTwoWithFactors(t, mapProviders{
		acme.ID:   {Issuer: acmeIDP.URL, ClientID: "platformkit", SecretRef: "ACME_SECRET", RedirectPath: "/api/v1/auth/oidc/callback"},
		globex.ID: {Issuer: globexIDP.URL, ClientID: "platformkit", SecretRef: "GLOBEX_SECRET", RedirectPath: "/api/v1/auth/oidc/callback"},
	}, mapSecrets{"ACME_SECRET": "acme-secret", "GLOBEX_SECRET": "globex-secret"})

	const ada = "ada@acme.example.com"
	const grace = "grace@acme.example.com"
	const karl = "karl@globex.example.com"
	adaID := person(t, conn, ada, contracts.RoleMember)
	person(t, conn, grace, contracts.RoleMember)
	karlID := personAt(t, conn, globex, karl, contracts.RoleMember)

	first := sessionCookie(federate(t, router, acmeIDP, host, ada, "ada-first"))
	if first == "" {
		t.Fatal("acme's leg signed nobody in before a factor existed, so nothing below tests a factor")
	}
	enrolFactor(t, router, first)

	// Globex, first, before the refusal below: a person there with no factor is
	// signed in by his own tenant's provider. Order matters — this is the state of
	// the process before anyone was refused, so a later failure cannot be blamed
	// on a cache that had not been warmed.
	karlBefore := sessionCookie(federate(t, router, globexIDP, secondHost, karl, "karl-before"))
	if karlBefore == "" {
		t.Fatal("globex's leg signed its own factor-less person in nowhere; the factor question is being " +
			"asked of something other than the account")
	}

	before := liveSessionRows(t, conn, acme, adaID)
	held := federate(t, router, acmeIDP, host, ada, "ada-held")
	if held.Code != http.StatusUnauthorized {
		t.Errorf("acme's leg for a person holding a factor = %d %s, want 401", held.Code, held.Body.String())
	}
	if sessionCookie(held) != "" {
		t.Error("the refused leg at acme set a session cookie")
	}
	if now := liveSessionRows(t, conn, acme, adaID); now != before {
		t.Errorf("the refused leg at acme holds %d sessions where %d were before it ran: a leg refused at "+
			"the door opens no row", now, before)
	}

	// The same tenant, the same issuer, a person who enrolled nothing.
	graceSession := sessionCookie(federate(t, router, acmeIDP, host, grace, "grace-open"))
	if graceSession == "" {
		t.Errorf("acme's leg signed its factor-less person nowhere (%d %s): the rule is about an account, "+
			"not about a tenant that grew a factor", held.Code, held.Body.String())
	}

	// And the other tenant, again, after the refusal: the factor that acme holds
	// is not globex's business, and globex's own person is still signed in.
	karlAfter := federate(t, router, globexIDP, secondHost, karl, "karl-after")
	if karlAfter.Code != http.StatusSeeOther || sessionCookie(karlAfter) == "" {
		t.Errorf("globex's own person = %d %s after acme refused its factor-holder, want 303 and a session: "+
			"the second half is asked of an account in a tenant, not of the process", karlAfter.Code, karlAfter.Body.String())
	}
	if got, want := liveSessionRows(t, conn, globex, karlID), 2; got != want {
		t.Errorf("globex's person holds %d live sessions after two legs that each completed, want %d — and "+
			"the acme refusal appears to have reached across the tenant boundary if this is short", got, want)
	}
}
