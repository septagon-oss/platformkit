package internal_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	"github.com/septagon-oss/platformkit/modules/auth/internal"
	"github.com/septagon-oss/platformkit/modules/user"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// host is where the tests are served. It is a local name, so the session cookie
// is not marked Secure and the OIDC redirect is http — which is what a browser
// on a laptop needs and what the test issuer speaks.
const host = "acme.localhost"

// mailbox is where every set-password link this file causes ends up, and
// notices is what the application told people inside itself. mount replaces
// both, so each holds what the application under test produced and nothing from
// the test before; these cases do not run in parallel, and a mailbox shared
// across two applications would be a test reading somebody else's mail.
//
// They are two because the link is in one of them and not the other, which is
// the property: a notification is an ordinary row and a token in one is a live
// credential sitting in a table.
var (
	mailbox = &authtest.Mailbox{}
	notices = &authtest.Notices{}
	// subs is the auth module's subscriptions, as mount registered them. There
	// is no worker in these tests, so a case that needs one drives them itself.
	// See worker.
	subs []events.Subscription
)

// site is the tenant loader for this file: one host, one tenant.
type site struct{}

func (site) ByHost(_ context.Context, _ db.Tx[db.System], h string) (tenancy.Tenant, error) {
	if h != host {
		return tenancy.Tenant{}, tenancy.ErrNoSuchHost
	}
	return acme, nil
}

// mount builds the application's HTTP surface around the real auth module.
func mount(t *testing.T, oidc auth.OIDC) (chi.Router, *db.Conn, contracts.Auth) {
	t.Helper()
	_, conn := dbtest.Schema(t)
	return mountOn(t, conn, oidc)
}

// mountOn is mount over a connection the caller opened, which is what the
// soft-delay case needs: it watches the pool from the outside, and a pool it
// cannot name is a pool it cannot watch.
func mountOn(t *testing.T, conn *db.Conn, oidc auth.OIDC) (chi.Router, *db.Conn, contracts.Auth) {
	t.Helper()
	mailbox, notices = &authtest.Mailbox{}, &authtest.Notices{}
	users, userModule := user.Module(user.Deps{})
	svc, authModule := auth.Module(auth.Deps{
		Users: users, Notify: notices, Mailer: mailbox, Hosts: authtest.Host(host),
		OIDC: oidc, PublicHost: host,
	})
	subs = authModule.Subscriptions
	seed(t, conn, svc, acme)

	api, router := httpx.New(httpx.Options{
		PublicHost: host, Tenants: site{}, Conn: conn,
		Authorize: svc, Authenticate: svc.Authenticate,
		Log: slog.New(slog.DiscardHandler),
	})
	// The catalogue the kernel would have declared, so that the roles route can
	// check a permission against it: kit/app reads it off the manifests before
	// any module registers anything, and this is that line.
	api.Declare([]tenancy.Grant{
		{Permission: contracts.PermissionRoleManage},
		{Permission: usercontracts.PermissionUserRead},
		{Permission: usercontracts.PermissionUserManage},
		{Permission: "tenant:manage", Operator: true},
	})
	authModule.Routes(api)
	userModule.Routes(api)
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the mounted routes do not declare themselves: %v", err)
	}
	return router, conn, svc
}

// person invites somebody and gives them a password, through the user module.
func person(t *testing.T, conn *db.Conn, email string, roles ...string) uuid.UUID {
	t.Helper()
	users := realUsers()
	var id uuid.UUID
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
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
		t.Fatalf("create %s: %v", email, err)
	}
	return id
}

// worker runs the module's subscriptions over everything in the outbox, once,
// which is between them what kit/events' relay and Consume do in a deployment.
//
// The forgotten-password flow needs it, and that is the point of the flow: the
// public route publishes and nothing else, and the lookup, the token and the
// mail all happen here — where no stranger with a stopwatch can time them. A
// case that stopped at the response would be testing half of it.
func worker(t *testing.T, conn *db.Conn) {
	t.Helper()
	type pending struct {
		Name    string
		Payload []byte
	}
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var rows []pending
		if err := tx.DB().Table("platformkit_outbox").Order("created_at, id").Find(&rows).Error; err != nil {
			return err
		}
		for _, r := range rows {
			for _, s := range subs {
				if s.Name != r.Name {
					continue
				}
				err := s.Handler(ctx, tx, events.Event{
					ID: uuid.New(), Name: r.Name, TenantID: acme.ID, Payload: r.Payload,
				})
				if err != nil {
					return err
				}
			}
		}
		return tx.DB().Exec("DELETE FROM platformkit_outbox").Error
	})
	if err != nil {
		t.Fatalf("run the subscriptions: %v", err)
	}
}

// call sends one request. Whatever a test wants to say about where the request
// came from, it says in headers here.
func call(t *testing.T, r http.Handler, method, path, body string, edit ...func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "http://"+host+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for _, e := range edit {
		e(req)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// withSession attaches a session cookie, which is what makes a request both
// recognisable and subject to the cross-site check.
func withSession(value string) func(*http.Request) {
	return func(r *http.Request) {
		// The plain name: these tests are served at a local host, so the cookie
		// is not Secure and carries no __Host- prefix.
		r.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: value})
	}
}

// sessionCookie is the session id the response set, or "" if it set none.
func sessionCookie(w *httptest.ResponseRecorder) string {
	for _, c := range (&http.Response{Header: w.Header()}).Cookies() {
		if c.Name == httpx.CookieName(httpx.SessionCookie, false) && c.Value != "" {
			return c.Value
		}
	}
	return ""
}

// TestSigningInAndOut walks the whole browser story: a password becomes a
// cookie and a row, the cookie answers for the caller, and signing out removes
// both — after which the same cookie is nobody.
func TestSigningInAndOut(t *testing.T) {
	router, conn, _ := mount(t, auth.OIDC{})
	id := person(t, conn, "ada@acme.localhost", contracts.RoleAdmin)

	res := call(t, router, http.MethodPost, "/api/v1/auth/login",
		`{"email":"ada@acme.localhost","password":"`+authtest.Password+`"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("login = %d %s, want 200", res.Code, res.Body.String())
	}
	session := sessionCookie(res)
	if session == "" {
		t.Fatal("login set no session cookie")
	}
	cookie := (&http.Response{Header: res.Header()}).Cookies()[0]
	switch {
	case !cookie.HttpOnly:
		t.Error("the session cookie is readable from JavaScript")
	case cookie.SameSite != http.SameSiteLaxMode:
		t.Errorf("the session cookie is SameSite=%v, want Lax", cookie.SameSite)
	case cookie.Path != "/":
		t.Errorf("the session cookie is scoped to %q, want /", cookie.Path)
	case cookie.Secure:
		t.Error("the session cookie is Secure at a local host, where a browser would refuse it")
	}
	if !strings.Contains(res.Body.String(), `"`+id.String()+`"`) {
		t.Errorf("login answered %s, want the caller's identity", res.Body.String())
	}

	// The row is there, in this tenant — under the hash of the cookie, and not
	// under the cookie. What the table holds is not what the browser carries,
	// so a copy of it is a list of hashes rather than a set of live sessions.
	var rows int64
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Table("sessions").Where("id_hash = ?", contracts.Hash(session)).Count(&rows).Error
	})
	if err != nil || rows != 1 {
		t.Fatalf("the sessions table holds %d rows for the cookie's hash (%v)", rows, err)
	}
	// And nothing anywhere in it is the cookie itself.
	var plaintext int64
	err = db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Table("sessions").Where("id_hash = ?", contracts.Digest(session)).Count(&plaintext).Error
	})
	if err != nil || plaintext != 0 {
		t.Errorf("the sessions table stores the credential itself (%d rows, %v)", plaintext, err)
	}

	// The cookie answers for the caller.
	res = call(t, router, http.MethodGet, "/api/v1/auth/me", "", withSession(session))
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), "ada@acme.localhost") {
		t.Fatalf("me = %d %s, want the caller", res.Code, res.Body.String())
	}
	// And admin's wildcard reaches a permission this module never heard of.
	if !strings.Contains(res.Body.String(), `"*"`) {
		t.Errorf("me = %s, want admin's wildcard", res.Body.String())
	}

	// Signing out removes the row and clears the cookie.
	res = call(t, router, http.MethodPost, "/api/v1/auth/logout", "", withSession(session),
		func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "same-origin") })
	if res.Code != http.StatusOK {
		t.Fatalf("logout = %d %s, want 200", res.Code, res.Body.String())
	}
	if got := (&http.Response{Header: res.Header()}).Cookies(); len(got) == 0 || got[0].MaxAge >= 0 {
		t.Errorf("logout did not clear the cookie: %v", got)
	}
	if res = call(t, router, http.MethodGet, "/api/v1/auth/me", "", withSession(session)); res.Code != http.StatusForbidden {
		t.Errorf("me after logout = %d %s, want 403", res.Code, res.Body.String())
	}
}

// TestAWrongPasswordIsA401AndSaysNothingElse.
func TestAWrongPasswordIsA401AndSaysNothingElse(t *testing.T) {
	router, conn, _ := mount(t, auth.OIDC{})
	person(t, conn, "ada@acme.localhost")

	wrong := call(t, router, http.MethodPost, "/api/v1/auth/login",
		`{"email":"ada@acme.localhost","password":"`+authtest.Wrong+`"}`)
	unknown := call(t, router, http.MethodPost, "/api/v1/auth/login",
		`{"email":"nobody@acme.localhost","password":"`+authtest.Password+`"}`)
	if wrong.Code != http.StatusUnauthorized || unknown.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password = %d, unknown address = %d; both are 401", wrong.Code, unknown.Code)
	}
	// The same body but for the request id, which every problem carries and
	// which is the caller's own. Anything else in it would be the difference
	// between "no such account" and "wrong password", which is the whole thing
	// this refusal exists not to say.
	if a, b := withoutInstance(wrong.Body.String()), withoutInstance(unknown.Body.String()); a != b {
		t.Errorf("the two answers differ:\n  %s\n  %s", a, b)
	}
	if sessionCookie(wrong) != "" || sessionCookie(unknown) != "" {
		t.Error("a failed login set a session cookie")
	}
}

// withoutInstance drops the request id from a problem body, which is the one
// field two occurrences of the same problem are entitled to differ in.
func withoutInstance(body string) string {
	var p map[string]any
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		return body
	}
	delete(p, "instance")
	out, _ := json.Marshal(p)
	return string(out)
}

// TestACrossSiteWriteWithASessionCookieIsRefused. A cookie is attached by the
// browser whichever page made the request, so a session cookie is a credential
// the caller did not choose to present; the browser's own account of where the
// request came from is what says whether they meant it.
func TestACrossSiteWriteWithASessionCookieIsRefused(t *testing.T) {
	router, conn, _ := mount(t, auth.OIDC{})
	person(t, conn, "ada@acme.localhost", contracts.RoleAdmin)
	res := call(t, router, http.MethodPost, "/api/v1/auth/login",
		`{"email":"ada@acme.localhost","password":"`+authtest.Password+`"}`)
	session := sessionCookie(res)

	for _, tt := range []struct {
		name    string
		headers map[string]string
		want    int
	}{
		{"a page on another site", map[string]string{"Sec-Fetch-Site": "cross-site"}, http.StatusForbidden},
		{"a page on another origin", map[string]string{"Origin": "https://evil.example"}, http.StatusForbidden},
		{"the application itself", map[string]string{"Sec-Fetch-Site": "same-origin"}, http.StatusOK},
		{"an address bar", map[string]string{"Sec-Fetch-Site": "none"}, http.StatusOK},
		{"our own origin, on an older browser", map[string]string{"Origin": "http://" + host}, http.StatusOK},
		{"a client that is not a browser", nil, http.StatusOK},
	} {
		t.Run(tt.name, func(t *testing.T) {
			res := call(t, router, http.MethodPost, "/api/v1/auth/logout", "", withSession(session),
				func(r *http.Request) {
					for k, v := range tt.headers {
						r.Header.Set(k, v)
					}
				})
			if res.Code != tt.want {
				t.Errorf("logout from %s = %d %s, want %d", tt.name, res.Code, res.Body.String(), tt.want)
			}
			if tt.want == http.StatusForbidden && !strings.Contains(res.Body.String(), "csrf") {
				t.Errorf("the refusal does not say why: %s", res.Body.String())
			}
			if res.Code == http.StatusOK {
				// Signing out worked, so sign back in for the next case.
				session = sessionCookie(call(t, router, http.MethodPost, "/api/v1/auth/login",
					`{"email":"ada@acme.localhost","password":"`+authtest.Password+`"}`))
			}
		})
	}

	// A read is never refused: nothing changes, so there is nothing to forge.
	if res := call(t, router, http.MethodGet, "/api/v1/auth/me", "", withSession(session),
		func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }); res.Code != http.StatusOK {
		t.Errorf("a cross-site read = %d, want 200", res.Code)
	}
}

// TestSingleSignOnRoundTrip drives both legs against a real issuer: discovery,
// a JWKS, a signed id token, PKCE and a state cookie. The application creates
// nobody: an address this tenant does not have is refused.
func TestSingleSignOnRoundTrip(t *testing.T) {
	issuer := authtest.NewIssuer(t)
	router, conn, _ := mount(t, auth.OIDC{
		Issuer: issuer.URL, ClientID: "platformkit", ClientSecret: "secret",
		RedirectPath: "/api/v1/auth/oidc/callback",
	})
	person(t, conn, "ada@acme.localhost", contracts.RoleMember)

	start := func(t *testing.T) (state, nonce, cookie string) {
		t.Helper()
		res := call(t, router, http.MethodGet, "/api/v1/auth/oidc/start", "")
		if res.Code != http.StatusSeeOther {
			t.Fatalf("start = %d %s, want 303", res.Code, res.Body.String())
		}
		to, err := url.Parse(res.Header().Get("Location"))
		if err != nil {
			t.Fatalf("the redirect is not a URL: %v", err)
		}
		if to.Query().Get("code_challenge_method") != "S256" || to.Query().Get("code_challenge") == "" {
			t.Errorf("the redirect carries no PKCE challenge: %s", to)
		}
		for _, c := range (&http.Response{Header: res.Header()}).Cookies() {
			if c.Name == "platformkit_oidc" {
				cookie = c.Value
			}
		}
		if cookie == "" {
			t.Fatal("start set no state cookie")
		}
		return to.Query().Get("state"), to.Query().Get("nonce"), cookie
	}

	callback := func(t *testing.T, code, state, cookie string) *httptest.ResponseRecorder {
		t.Helper()
		return call(t, router, http.MethodGet,
			"/api/v1/auth/oidc/callback?code="+url.QueryEscape(code)+"&state="+url.QueryEscape(state), "",
			func(r *http.Request) { r.AddCookie(&http.Cookie{Name: "platformkit_oidc", Value: cookie}) })
	}

	t.Run("a verified address this tenant knows is signed in", func(t *testing.T) {
		state, nonce, cookie := start(t)
		issuer.Issue("code-ada", "ada@acme.localhost", true, "platformkit", nonce)
		res := callback(t, "code-ada", state, cookie)
		if res.Code != http.StatusSeeOther {
			t.Fatalf("callback = %d %s, want 303", res.Code, res.Body.String())
		}
		session := sessionCookie(res)
		if session == "" {
			t.Fatal("the callback set no session cookie")
		}
		if res := call(t, router, http.MethodGet, "/api/v1/auth/me", "", withSession(session)); res.Code != http.StatusOK ||
			!strings.Contains(res.Body.String(), "ada@acme.localhost") {
			t.Errorf("me after single sign-on = %d %s", res.Code, res.Body.String())
		}
	})

	t.Run("an address this tenant does not have is refused", func(t *testing.T) {
		state, nonce, cookie := start(t)
		issuer.Issue("code-grace", "grace@elsewhere.example", true, "platformkit", nonce)
		res := callback(t, "code-grace", state, cookie)
		if res.Code != http.StatusForbidden {
			t.Fatalf("callback for an unknown address = %d %s, want 403", res.Code, res.Body.String())
		}
		if sessionCookie(res) != "" {
			t.Error("an unknown address got a session; nobody is created here")
		}
	})

	t.Run("an unverified address is refused", func(t *testing.T) {
		state, nonce, cookie := start(t)
		issuer.Issue("code-unverified", "ada@acme.localhost", false, "platformkit", nonce)
		if res := callback(t, "code-unverified", state, cookie); res.Code != http.StatusForbidden {
			t.Errorf("an unverified address = %d %s, want 403", res.Code, res.Body.String())
		}
	})

	t.Run("a state that did not come from here is refused", func(t *testing.T) {
		_, nonce, cookie := start(t)
		issuer.Issue("code-forged", "ada@acme.localhost", true, "platformkit", nonce)
		if res := callback(t, "code-forged", "somebody-elses-state", cookie); res.Code != http.StatusForbidden {
			t.Errorf("a forged state = %d %s, want 403", res.Code, res.Body.String())
		}
	})
}

// TestTheOIDCRoutesExistOnlyWhenAProviderDoes: a route that would answer "this
// application has no identity provider" is a route with nothing to say.
func TestTheOIDCRoutesExistOnlyWhenAProviderDoes(t *testing.T) {
	router, _, _ := mount(t, auth.OIDC{})
	if res := call(t, router, http.MethodGet, "/api/v1/auth/oidc/start", ""); res.Code != http.StatusNotFound {
		t.Errorf("oidc/start with no provider configured = %d, want 404", res.Code)
	}
}

// TestTheRolesRoutesAreGuardedAndChecked.
//
// A role is what everybody else in the tenant may do, so the two routes are the
// only ones in this module that name a permission. What they refuse is the
// interesting part: a permission no module defines would be a grant that can
// never be exercised and reads like one that can, and an operator permission
// here would be a grant the kernel refuses at every route it guards.
func TestTheRolesRoutesAreGuardedAndChecked(t *testing.T) {
	router, conn, _ := mount(t, auth.OIDC{})
	person(t, conn, "grace@acme.localhost", contracts.RoleMember)
	person(t, conn, "ada@acme.localhost", contracts.RoleAdmin)

	member := signIn(t, router, "grace@acme.localhost")
	if res := call(t, router, http.MethodGet, "/api/v1/auth/roles", "", withSession(member)); res.Code != http.StatusForbidden {
		t.Errorf("a member listing the roles = %d %s, want 403", res.Code, res.Body.String())
	}

	admin := signIn(t, router, "ada@acme.localhost")
	res := call(t, router, http.MethodGet, "/api/v1/auth/roles", "", withSession(admin))
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), contracts.RoleAdmin) {
		t.Fatalf("GET roles = %d %s, want the two a tenant starts with", res.Code, res.Body.String())
	}

	put := func(name, body string) *httptest.ResponseRecorder {
		return call(t, router, http.MethodPut, "/api/v1/auth/roles/"+name, body, withSession(admin),
			func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "same-origin") })
	}
	if res := put("editor", `{"permissions":["user:read"]}`); res.Code != http.StatusOK ||
		!strings.Contains(res.Body.String(), "user:read") {
		t.Fatalf("PUT a role = %d %s, want 200", res.Code, res.Body.String())
	}
	// The name is normalised the way the user module normalises the roles a
	// person holds, so "Editor" and "editor" are one role rather than two rows
	// nobody's user list matches.
	if res := put("EDITOR", `{"permissions":["user:read","user:manage"]}`); res.Code != http.StatusOK ||
		!strings.Contains(res.Body.String(), `"name":"editor"`) {
		t.Errorf("PUT EDITOR = %d %s, want the same role", res.Code, res.Body.String())
	}
	for _, tt := range []struct{ name, body, want string }{
		{"editor", `{"permissions":["ghost:read"]}`, "ghost:read"},
		{"operator", `{"permissions":["tenant:manage"]}`, "tenant:manage"},
		{"1editor", `{"permissions":[]}`, "lower-case"},
	} {
		res := put(tt.name, tt.body)
		if res.Code != http.StatusUnprocessableEntity {
			t.Errorf("PUT %s %s = %d %s, want 422", tt.name, tt.body, res.Code, res.Body.String())
		}
		if !strings.Contains(res.Body.String(), tt.want) {
			t.Errorf("the refusal does not name %q: %s", tt.want, res.Body.String())
		}
	}
	// And the refused writes changed nothing.
	res = call(t, router, http.MethodGet, "/api/v1/auth/roles", "", withSession(admin))
	if strings.Contains(res.Body.String(), "ghost:read") || strings.Contains(res.Body.String(), "tenant:manage") {
		t.Errorf("a refused write reached the table: %s", res.Body.String())
	}
}

// TestChangingAPasswordEndsTheOtherSessionsAndNotThisOne, over HTTP, because
// "this one" is the cookie the request carried and nothing below the handler
// knows which that is.
func TestChangingAPasswordEndsTheOtherSessionsAndNotThisOne(t *testing.T) {
	router, conn, _ := mount(t, auth.OIDC{})
	person(t, conn, "ada@acme.localhost", contracts.RoleMember)
	here, elsewhere := signIn(t, router, "ada@acme.localhost"), signIn(t, router, "ada@acme.localhost")

	change := func(session, current string) *httptest.ResponseRecorder {
		return call(t, router, http.MethodPost, "/api/v1/auth/password",
			`{"current":"`+current+`","new":"a different passphrase"}`, withSession(session),
			func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "same-origin") })
	}
	if res := change(here, authtest.Wrong); res.Code != http.StatusUnauthorized {
		t.Fatalf("the wrong current password = %d %s, want 401", res.Code, res.Body.String())
	}
	if res := call(t, router, http.MethodGet, "/api/v1/auth/me", "", withSession(elsewhere)); res.Code != http.StatusOK {
		t.Errorf("a refused change ended the other session: %d", res.Code)
	}

	if res := change(here, authtest.Password); res.Code != http.StatusOK {
		t.Fatalf("the change = %d %s, want 200", res.Code, res.Body.String())
	}
	if res := call(t, router, http.MethodGet, "/api/v1/auth/me", "", withSession(here)); res.Code != http.StatusOK {
		t.Errorf("the session that asked for the change was ended: %d", res.Code)
	}
	if res := call(t, router, http.MethodGet, "/api/v1/auth/me", "", withSession(elsewhere)); res.Code != http.StatusForbidden {
		t.Errorf("the other session survived the change: %d", res.Code)
	}
}

// TestTheForgottenPasswordRouteSaysTheSameThingToEverybody, and the link it
// mails works once and ends every session.
func TestTheForgottenPasswordRouteSaysTheSameThingToEverybody(t *testing.T) {
	router, conn, _ := mount(t, auth.OIDC{})
	person(t, conn, "ada@acme.localhost", contracts.RoleMember)
	live := signIn(t, router, "ada@acme.localhost")

	forgot := func(email string) *httptest.ResponseRecorder {
		return call(t, router, http.MethodPost, "/api/v1/auth/password/forgot", `{"email":"`+email+`"}`)
	}
	known, unknown := forgot("ada@acme.localhost"), forgot("nobody@acme.localhost")
	if known.Code != http.StatusOK || unknown.Code != http.StatusOK {
		t.Fatalf("known = %d, unknown = %d; both are 200", known.Code, unknown.Code)
	}
	if a, b := withoutInstance(known.Body.String()), withoutInstance(unknown.Body.String()); a != b {
		t.Errorf("the two answers differ, which is an enumeration oracle:\n  %s\n  %s", a, b)
	}
	// Nothing has been mailed yet: the route published, and the worker is what
	// decides whether anybody is there.
	if got := mailbox.Sent(); len(got) != 0 {
		t.Fatalf("the request path sent %d messages; it sends none", len(got))
	}
	worker(t, conn)
	sent := mailbox.Sent()
	if len(sent) != 1 {
		t.Fatalf("%d links were mailed, want one — for the address that is here", len(sent))
	}
	// The link is absolute, on this tenant's own host, and it is in the message
	// and in no row.
	if !strings.Contains(sent[0].Body, "http://"+host+internal.ResetPath+"?token=") {
		t.Errorf("the mailed link is not this tenant's own absolute URL: %q", sent[0].Body)
	}

	reset := call(t, router, http.MethodPost, "/api/v1/auth/password/reset",
		`{"token":"`+authtest.TokenIn(sent[0].Body)+`","new":"a different passphrase"}`)
	if reset.Code != http.StatusOK {
		t.Fatalf("the reset = %d %s, want 200", reset.Code, reset.Body.String())
	}
	// Every session, including one opened before any of this.
	if res := call(t, router, http.MethodGet, "/api/v1/auth/me", "", withSession(live)); res.Code != http.StatusForbidden {
		t.Errorf("a session survived a reset: %d", res.Code)
	}
	// And the token is spent.
	again := call(t, router, http.MethodPost, "/api/v1/auth/password/reset",
		`{"token":"`+authtest.TokenIn(sent[0].Body)+`","new":"another passphrase"}`)
	if again.Code != http.StatusUnauthorized {
		t.Errorf("the link worked twice: %d %s", again.Code, again.Body.String())
	}
	// The new password signs in.
	res := call(t, router, http.MethodPost, "/api/v1/auth/login",
		`{"email":"ada@acme.localhost","password":"a different passphrase"}`)
	if res.Code != http.StatusOK {
		t.Errorf("the reset password does not sign in: %d %s", res.Code, res.Body.String())
	}
}

// TestTheCookieCarriesTheHostPrefixWhenItIsSecure.
//
// __Host- is a rule the browser enforces: a cookie named with it is only
// accepted with Secure, with Path=/ and with no Domain, and a page on another
// host cannot set one the browser will send here. Every tenant is reached at
// its own host, often as siblings under one domain, so a customer's host being
// unable to forge another's session cookie is not a hypothetical.
func TestTheCookieCarriesTheHostPrefixWhenItIsSecure(t *testing.T) {
	for _, tt := range []struct{ host, want string }{
		{"acme.localhost", "platformkit_session"},
		{"acme.example.com", "__Host-platformkit_session"},
	} {
		t.Run(tt.host, func(t *testing.T) {
			if got := httpx.CookieName(httpx.SessionCookie, !config.Local(tt.host)); got != tt.want {
				t.Errorf("at %s the cookie is %q, want %q", tt.host, got, tt.want)
			}
		})
	}
	// And the kernel recognises both, because a deployment is one or the other.
	for _, name := range []string{"platformkit_session", "__Host-platformkit_session"} {
		r := httptest.NewRequest(http.MethodGet, "https://acme.example.com/", nil)
		r.AddCookie(&http.Cookie{Name: name, Value: "present"})
		if _, ok := httpx.SessionCookieOf(r); !ok {
			t.Errorf("the kernel does not recognise %s", name)
		}
	}
}

// signIn is a session cookie for somebody whose password is authtest.Password.
func signIn(t *testing.T, router chi.Router, email string) string {
	t.Helper()
	res := call(t, router, http.MethodPost, "/api/v1/auth/login",
		`{"email":"`+email+`","password":"`+authtest.Password+`"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("login as %s = %d %s", email, res.Code, res.Body.String())
	}
	session := sessionCookie(res)
	if session == "" {
		t.Fatalf("login as %s set no cookie", email)
	}
	return session
}

// TestACrossSiteSignInIsRefused is the half of the cross-site check the kernel
// cannot make. Its middleware guards a request that carries a session cookie,
// because a cookie is a credential the browser attached on the caller's behalf;
// a sign-in carries none, so it goes straight through.
//
// That is wrong for this one route, because a sign-in mints a credential rather
// than spending one. Another site posts here with its own account's password,
// the visitor is left signed in as somebody else, and whatever they do next —
// a document they upload, an address they type — happens in the attacker's
// account. So the route asks the same question the middleware asks.
func TestACrossSiteSignInIsRefused(t *testing.T) {
	router, conn, _ := mount(t, auth.OIDC{})
	person(t, conn, "ada@acme.localhost", contracts.RoleAdmin)
	credentials := `{"email":"ada@acme.localhost","password":"` + authtest.Password + `"}`

	for _, tt := range []struct {
		name    string
		headers map[string]string
		want    int
	}{
		{"a form on another site", map[string]string{"Sec-Fetch-Site": "cross-site"}, http.StatusForbidden},
		{"a form on another origin", map[string]string{"Origin": "https://evil.example"}, http.StatusForbidden},
		{"the sign-in page itself", map[string]string{"Sec-Fetch-Site": "same-origin"}, http.StatusOK},
		{"an address bar", map[string]string{"Sec-Fetch-Site": "none"}, http.StatusOK},
		{"our own origin, on an older browser", map[string]string{"Origin": "http://" + host}, http.StatusOK},
		// A client that is not a browser presents what it presents on purpose,
		// and this is how every API client and the e2e suite sign in.
		{"a client that is not a browser", nil, http.StatusOK},
	} {
		t.Run(tt.name, func(t *testing.T) {
			res := call(t, router, http.MethodPost, "/api/v1/auth/login", credentials,
				func(r *http.Request) {
					for k, v := range tt.headers {
						r.Header.Set(k, v)
					}
				})
			if res.Code != tt.want {
				t.Errorf("signing in from %s = %d %s, want %d", tt.name, res.Code, res.Body.String(), tt.want)
			}
			if tt.want == http.StatusForbidden && sessionCookie(res) != "" {
				t.Error("the refused sign-in set a session cookie anyway")
			}
		})
	}
}

// TestSpendingAResetTokenIsRatedTooAsWellAsAskingForOne. Asking for a link was
// capped and spending one was not, so the token could be presented in a loop at
// the speed of the network. It is 256 bits from crypto/rand, so guessing it is
// not a real attack — but "the number is large" was the only thing stopping the
// traffic, and a cap costs nothing.
func TestSpendingAResetTokenIsRatedTooAsWellAsAskingForOne(t *testing.T) {
	router, conn, _ := mount(t, auth.OIDC{})
	person(t, conn, "ada@acme.localhost")

	body := `{"token":"a-token-that-is-not-one","new":"correct horse battery staple 2"}`
	for i := range contracts.ResetRedemptions {
		// Every one of them is refused for the token, which is the answer a
		// wrong token gets and the answer this route gives about every token.
		if res := call(t, router, http.MethodPost, "/api/v1/auth/password/reset", body); res.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d = %d %s, want 401", i+1, res.Code, res.Body.String())
		}
	}
	res := call(t, router, http.MethodPost, "/api/v1/auth/password/reset", body)
	if res.Code != http.StatusTooManyRequests {
		t.Errorf("attempt %d = %d %s, want 429", contracts.ResetRedemptions+1, res.Code, res.Body.String())
	}
}
