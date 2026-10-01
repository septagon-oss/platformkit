package internal_test

// review 4 (decision 0039: HIGHs only). Two claims about the bearer key that the
// delivery makes about itself, tried at the router.
//
// The first is the ceiling. modules/auth/README.md says "A key is a narrowed
// credential, never an escalated one", and tenancy.Principal.Permissions says "A
// non-nil list is a ceiling, not an addition. An authorizer may grant what is on
// this list and nothing else, whatever the holder's roles say". kit/httpx
// authorize.go writes that line after the kindSignedIn branch has already called
// next, so it is reached only by an operation that asks a permission — and every
// route that manages a person's own credential asks none, because
// "about the caller themselves" is the declaration every session, factor and
// token route carries. One of those routes is POST /tokens. So a key scoped to
// one read permission can mint a second key carrying any permission its holder's
// roles grant: the narrow credential widens itself, which is the escalation the
// sentence above says cannot happen. A deploy bot whose read-only key leaks in a
// log becomes an administrator's key, and nothing in the trail says the person
// asked for it beyond an issue event naming the caller's user id.
//
// The second is the word "expiring" in the brief's item 4. Nothing in the tree
// ran a key whose expiry had passed, and the ceiling on a requested lifetime is
// prose in contracts/tokens.go until this file.
//
// The two cases are independent of each other; the first fails at this HEAD and
// the second passes, and the second fails if the expiry check, the ceiling or the
// default is removed.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/user"
)

// TestAScopedKeyCannotMintAKeyWiderThanItsOwnScope is the ceiling. The request is
// the same request in both halves — one address, one body, one valid caller — and
// only the credential differs, so a pass cannot come from the input being
// acceptable and a failure cannot come from it being unacceptable either.
func TestAScopedKeyCannotMintAKeyWiderThanItsOwnScope(t *testing.T) {
	router, conn, _ := mount(t, auth.OIDC{})
	person(t, conn, "ada@acme.localhost", contracts.RoleAdmin)
	ada := signIn(t, router, "ada@acme.localhost")

	res := call(t, router, http.MethodPost, "/api/v1/auth/tokens",
		`{"name":"Read only","scopes":["user:read"]}`, withSession(ada))
	if res.Code != http.StatusCreated {
		t.Fatalf("minting a key scoped to one read permission = %d %s, want 201", res.Code, res.Body.String())
	}
	var narrow struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &narrow); err != nil {
		t.Fatalf("read the issue response: %v (%s)", err, res.Body.String())
	}

	// The key the read-only key would like to be: role:manage, which its holder
	// holds through the administrator role and which opens the roles route.
	const wider = `{"name":"Wider","scopes":["role:manage"]}`

	// The attempt, through the narrow credential. Whatever its shape, a key that
	// does not carry role:manage may not hand one out: the ceiling is the point of
	// the scopes, and an issue that consults the holder's roles rather than the
	// presenting credential answers a question about the wrong caller.
	escalated := call(t, router, http.MethodPost, "/api/v1/auth/tokens", wider, bearer(narrow.Token))
	if escalated.Code < 400 {
		t.Errorf("a key scoped to user:read minted a key scoped to role:manage: %d %s — "+
			"the scope is decoration, because POST /api/v1/auth/tokens declares SignedIn and "+
			"the credential ceiling in kit/httpx/authorize.go is checked after that kind returns",
			escalated.Code, escalated.Body.String())
	}
	if strings.Contains(escalated.Body.String(), "pkit_") {
		t.Errorf("the refused issue answered with a credential anyway: %s", escalated.Body.String())
	}
	// And it wrote nothing: the person's own list still holds the one key that was
	// asked for with their cookie. A refusal that leaves a live key behind is a
	// refusal that issued one.
	if got := keys(t, listKeys(t, router, ada)); got != 1 {
		t.Errorf("the refused issue left %d keys on the account, want the 1 minted with the session cookie", got)
	}

	// The control: the same body through the person's own session is a 201. It runs
	// last so that its success cannot stand in for the attempt above, and its only
	// job is to show that the refusal asked for is about the credential and not
	// about the input.
	if mine := call(t, router, http.MethodPost, "/api/v1/auth/tokens", wider, withSession(ada)); mine.Code != http.StatusCreated {
		t.Errorf("the person's own session minting a role:manage key = %d %s, want 201 — "+
			"this case is about the narrowed credential, not about refusing role:manage", mine.Code, mine.Body.String())
	}
}

// TestANarrowKeyCannotTakeTheRecoveryCodesOfItsHolder is the same missing ceiling
// at the route where it does the most damage: /factors/recovery/rotate is
// SignedIn, retires every code the person holds and returns a fresh set in its
// response. A credential scoped to one read permission that walks into that route
// leaves the person with no recovery codes and walks out holding the only set that
// works — which is a second factor an attacker now owns, reached from a key that
// was never given permission to anything but a read.
func TestANarrowKeyCannotTakeTheRecoveryCodesOfItsHolder(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, func(d *auth.Deps) {
		d.FactorKey = "a factor key this deployment set"
	})
	const email = "ada@acme.localhost"
	person(t, conn, email, contracts.RoleAdmin)
	login := call(t, router, http.MethodPost, "/api/v1/auth/login",
		`{"email":"`+email+`","password":"`+authtest.Password+`"}`)
	if login.Code != http.StatusOK {
		t.Fatalf("password sign-in = %d %s, want 200", login.Code, login.Body.String())
	}
	cookie := sessionCookie(login)
	begin := call(t, router, http.MethodPost, "/api/v1/auth/factors/totp/begin", "", withSession(cookie))
	var enrolment contracts.TOTPEnrolment
	if err := json.Unmarshal(begin.Body.Bytes(), &enrolment); err != nil {
		t.Fatalf("read the enrolment: %v (%s)", err, begin.Body.String())
	}
	finish := call(t, router, http.MethodPost, "/api/v1/auth/factors/totp/finish",
		`{"secret":"`+enrolment.Secret+`","code":"`+codeFor(t, enrolment.Secret, db.Now())+`"}`, withSession(cookie))
	if finish.Code != http.StatusCreated {
		t.Fatalf("finishing an enrolment = %d %s, want 201", finish.Code, finish.Body.String())
	}
	issued := call(t, router, http.MethodPost, "/api/v1/auth/tokens",
		`{"name":"Read only","scopes":["user:read"]}`, withSession(cookie))
	var key struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(issued.Body.Bytes(), &key); err != nil {
		t.Fatalf("read the issue response: %v (%s)", err, issued.Body.String())
	}

	rotated := call(t, router, http.MethodPost, "/api/v1/auth/factors/recovery/rotate", "", bearer(key.Token))
	if rotated.Code < 400 {
		t.Errorf("a key scoped to user:read rotated the holder's recovery codes: %d — a credential "+
			"narrower than its holder owns that holder's second factor by taking its codes", rotated.Code)
	}
	if got := hexGroup.FindString(rotated.Body.String()); got != "" {
		t.Errorf("the rotated set was handed to the narrowed credential: a live recovery code %s is in a response "+
			"answered to a key scoped to user:read", got)
	}
}

// TestANarrowKeyCannotSignThePersonOutOfEveryBrowser is F1 at its cheapest and its
// most visible: POST /api/v1/auth/sessions/revoke-all is SignedIn, and a request
// that arrives on a bearer credential carries no cookie, so the session it is told
// to keep is the nil UUID — whose hash is what sessions.go deliberately uses to say
// "keep none". A credential scoped to one read permission therefore signs the person
// out of every device they are on, which is the availability half of the same hole.
func TestANarrowKeyCannotSignThePersonOutOfEveryBrowser(t *testing.T) {
	router, conn, _ := mount(t, auth.OIDC{})
	person(t, conn, "ada@acme.localhost", contracts.RoleAdmin)
	ada := signIn(t, router, "ada@acme.localhost")
	res := call(t, router, http.MethodPost, "/api/v1/auth/tokens",
		`{"name":"Read only","scopes":["user:read"]}`, withSession(ada))
	var key struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &key); err != nil {
		t.Fatalf("read the issue response: %v (%s)", err, res.Body.String())
	}

	// A second device, which is the thing at stake.
	elsewhere := signIn(t, router, "ada@acme.localhost")
	if still := call(t, router, http.MethodGet, "/api/v1/auth/roles", "", withSession(elsewhere)); still.Code != http.StatusOK {
		t.Fatalf("the second device's session = %d, want 200 before the attempt", still.Code)
	}

	attacked := call(t, router, http.MethodPost, "/api/v1/auth/sessions/revoke-all", "", bearer(key.Token))
	if attacked.Code < 400 {
		t.Errorf("a key scoped to user:read called sign-out-everywhere: %d — the bearer request names "+
			"no session to keep, so the command it reaches is told to keep none", attacked.Code)
	}
	if gone := call(t, router, http.MethodGet, "/api/v1/auth/roles", "", withSession(elsewhere)); gone.Code != http.StatusOK {
		t.Errorf("the other device's session died at %d, want it still working: a credential scoped to a "+
			"read ended the person's own browsers", gone.Code)
	}
}

// hexGroup is one printed recovery code: four groups of eight hex characters.
var hexGroup = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{8}-[0-9a-f]{8}-[0-9a-f]{8}`)

func listKeys(t *testing.T, router http.Handler, cookie string) string {
	t.Helper()
	res := call(t, router, http.MethodGet, "/api/v1/auth/tokens", "", withSession(cookie))
	if res.Code != http.StatusOK {
		t.Fatalf("listing the person's own keys = %d %s, want 200", res.Code, res.Body.String())
	}
	return res.Body.String()
}

// TestAKeyDiesAtItsExpiryAndNeverOutlivesTheCeiling pins the brief's word
// "expiring" for the credential the brief's item 4 asked for: a requested
// lifetime beyond contracts.APITokenMaxLifetime is refused and writes nothing, a
// key with no requested lifetime dies at the default, and a key whose lifetime has
// run out is nobody at the route it used to open.
func TestAKeyDiesAtItsExpiryAndNeverOutlivesTheCeiling(t *testing.T) {
	router, conn, _ := mount(t, auth.OIDC{})
	person(t, conn, "ada@acme.localhost", contracts.RoleAdmin)
	ada := signIn(t, router, "ada@acme.localhost")

	issue := func(body string) (int, string, time.Time) {
		t.Helper()
		res := call(t, router, http.MethodPost, "/api/v1/auth/tokens", body, withSession(ada))
		var out struct {
			Token     string    `json:"token"`
			ExpiresAt time.Time `json:"expiresAt"`
		}
		if res.Code == http.StatusCreated {
			if err := json.Unmarshal(res.Body.Bytes(), &out); err != nil {
				t.Fatalf("read the issue response: %v (%s)", err, res.Body.String())
			}
		}
		return res.Code, out.Token, out.ExpiresAt
	}

	// The ceiling. Nothing extends a bearer's life, so nothing may ask for more
	// than the ceiling once, either.
	code, _, _ := issue(fmt.Sprintf(`{"name":"Too long","scopes":["role:manage"],"expiresAt":%q}`,
		time.Now().UTC().Add(contracts.APITokenMaxLifetime+24*time.Hour).Format(time.RFC3339)))
	if code != http.StatusUnprocessableEntity {
		t.Errorf("asking for a key longer than %s = %d, want 422", contracts.APITokenMaxLifetime, code)
	}
	// The default, asked for by omitting the field rather than by a constant here.
	code, _, until := issue(`{"name":"Default","scopes":["role:manage"]}`)
	if code != http.StatusCreated {
		t.Fatalf("a key with no requested lifetime = %d, want 201", code)
	}
	if d := time.Until(until); d < contracts.APITokenDefaultLifetime-time.Hour || d > contracts.APITokenDefaultLifetime+time.Hour {
		t.Errorf("a key with no requested lifetime dies in %s, want the default %s", d, contracts.APITokenDefaultLifetime)
	}
	// And the expiry itself, which no request can produce on its own timetable: the
	// row is aged rather than waited for, by the amount of its own lifetime.
	code, token, until := issue(fmt.Sprintf(`{"name":"Short","scopes":["role:manage"],"expiresAt":%q}`,
		time.Now().UTC().Add(48*time.Hour).Format(time.RFC3339)))
	if code != http.StatusCreated || token == "" {
		t.Fatalf("a key with a two-day lifetime = %d, want 201", code)
	}
	if open := call(t, router, http.MethodGet, "/api/v1/auth/roles", "", bearer(token)); open.Code != http.StatusOK {
		t.Fatalf("a live key at the route its scope names = %d %s, want 200", open.Code, open.Body.String())
	}
	// Shift the row back by more than its remaining life. Both timestamps move by
	// the same amount, so api_tokens_expiry_after_issue still holds: this is a key
	// that was issued a long time ago and died last week, not an impossible row.
	if err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Exec("UPDATE api_tokens SET created_at = created_at - interval '40 days', " +
			"expires_at = expires_at - interval '40 days' " +
			"WHERE expires_at > now() + interval '24 hours' AND expires_at < now() + interval '72 hours'").Error
	}); err != nil {
		t.Fatalf("age the key: %v", err)
	}
	expired := call(t, router, http.MethodGet, "/api/v1/auth/roles", "", bearer(token))
	if expired.Code != http.StatusForbidden {
		t.Errorf("a key past its expires_at = %d %s, want 403 — an expiry nobody runs is a credential that never dies",
			expired.Code, expired.Body.String())
	}
	// The person's own session outlives the key, as it does for a revocation.
	if still := call(t, router, http.MethodGet, "/api/v1/auth/roles", "", withSession(ada)); still.Code != http.StatusOK {
		t.Errorf("a key dying cost the person their own session: %d", still.Code)
	}
}
