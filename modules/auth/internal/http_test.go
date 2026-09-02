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

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	"github.com/septagon-oss/platformkit/modules/user"
)

// host is where the tests are served. It is a local name, so the session cookie
// is not marked Secure and the OIDC redirect is http — which is what a browser
// on a laptop needs and what the test issuer speaks.
const host = "acme.localhost"

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
	users, userModule := user.Module(user.Deps{})
	svc, authModule := auth.Module(auth.Deps{Users: users, OIDC: oidc, PublicHost: host})
	seed(t, conn, svc, acme)

	api, router := httpx.New(httpx.Options{
		PublicHost: host, Tenants: site{}, Conn: conn,
		Authorize: svc, Authenticate: svc.Authenticate,
		Log: slog.New(slog.DiscardHandler),
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
	return func(r *http.Request) { r.AddCookie(&http.Cookie{Name: httpx.SessionCookie, Value: value}) }
}

// sessionCookie is the session id the response set, or "" if it set none.
func sessionCookie(w *httptest.ResponseRecorder) string {
	for _, c := range (&http.Response{Header: w.Header()}).Cookies() {
		if c.Name == httpx.SessionCookie && c.Value != "" {
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

	// The row is there, in this tenant.
	var rows int64
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Table("sessions").Where("id = ?", session).Count(&rows).Error
	})
	if err != nil || rows != 1 {
		t.Fatalf("the sessions table holds %d rows for the cookie (%v)", rows, err)
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
