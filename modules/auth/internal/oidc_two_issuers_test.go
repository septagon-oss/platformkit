package internal_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/cache"
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

// secondHost is the other customer's name for the same process.
const secondHost = "globex.localhost"

// twoSites is the host resolution the brief's test needs: two tenants, one
// process, and a request that arrives at whichever name the caller used.
type twoSites struct{}

func (twoSites) ByHost(_ context.Context, _ db.Tx[db.System], h string) (tenancy.Tenant, error) {
	switch h {
	case host:
		return acme, nil
	case secondHost:
		return globex, nil
	}
	return tenancy.Tenant{}, tenancy.ErrNoSuchHost
}

// mapProviders is the test's stand-in for the tenant module's row: the port is
// one method, so the double is a map, and that is the whole reason the port is
// declared over the capability rather than the tenant module's Service.
type mapProviders map[uuid.UUID]contracts.OIDCProvider

func (m mapProviders) ProviderOf(_ context.Context, tx db.Tx[db.Tenant]) (*contracts.OIDCProvider, bool, error) {
	settings, ok := m[db.TenantOf(tx).ID]
	if !ok {
		return nil, false, nil
	}
	return &settings, true, nil
}

// mapSecrets is the environment: a reference is a name, and a name with no value
// behind it resolves to nothing.
type mapSecrets map[string]string

func (m mapSecrets) Lookup(_ context.Context, ref string) (string, bool) {
	s, ok := m[ref]
	return s, ok && s != ""
}

// mountTwo mounts the real auth module with no installation-wide issuer at all:
// every provider a request can reach comes from the tenant the Host resolved,
// which is the arrangement no single-issuer process could express.
func mountTwo(t *testing.T, providers contracts.OIDCProviders, secrets contracts.Secrets) (chi.Router, *db.Conn) {
	t.Helper()
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	users, userModule := user.Module(user.Deps{
		Administration: &usercontracts.AdministrationFunc{Ask: auth.AdministeringRoles},
		Granting:       allowGranting,
	})
	svc, authModule := auth.Module(auth.Deps{
		Users: users, Notify: &authtest.Notices{}, Mailer: &authtest.Mailbox{},
		Hosts: authtest.Host(host), OIDCProviders: providers, Secrets: secrets,
		PublicHost: host,
	})
	seed(t, conn, acme)
	seed(t, conn, globex)

	api, router := httpx.New(httpx.Options{
		Cache:      cache.Memory("pkit"),
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

func get(t *testing.T, r http.Handler, at, path string, edit ...func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "http://"+at+path, nil)
	for _, e := range edit {
		e(req)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// start reads the state out of the redirect and the verifier out of the cookie
// the start set, which is everything a browser has at the point the identity
// provider takes over.
func start(t *testing.T, r http.Handler, at string) (location string, state string, cookie string, code int) {
	t.Helper()
	res := get(t, r, at, "/api/v1/auth/oidc/start")
	if res.Code != http.StatusSeeOther && res.Code != http.StatusNotFound {
		t.Fatalf("%s start = %d %s", at, res.Code, res.Body.String())
	}
	if res.Code != http.StatusSeeOther {
		return "", "", "", res.Code
	}
	to, err := url.Parse(res.Header().Get("Location"))
	if err != nil {
		t.Fatalf("the redirect is not a URL: %v", err)
	}
	for _, c := range (&http.Response{Header: res.Header()}).Cookies() {
		if c.Name == "platformkit_oidc" {
			cookie = c.Value
		}
	}
	if cookie == "" {
		t.Fatal("start set no state cookie")
	}
	return to.Query().Get("code_challenge"), to.Query().Get("state"), cookie, res.Code
}

func callback(t *testing.T, r http.Handler, at, code, state, cookie string) *httptest.ResponseRecorder {
	t.Helper()
	return get(t, r, at, "/api/v1/auth/oidc/callback?code="+url.QueryEscape(code)+"&state="+url.QueryEscape(state),
		func(req *http.Request) { req.AddCookie(&http.Cookie{Name: "platformkit_oidc", Value: cookie}) })
}

// TestTwoTenantsSignInAtTwoIssuersInOneProcess is the brief's own test, and the
// one claim of the design it can break: the discovery cache.
//
// One Service, one Provider, one process, two issuers. Acme's people are sent to
// acme's provider and Globex's to globex's; a code minted by the wrong one is
// refused, not quietly accepted by whichever provider this process happened to
// discover first. A cache keyed by anything other than the issuer — a single
// field, a process-wide default, a mutex around one provider — fails this case
// with a session minted at the wrong door, which is the failure the whole
// per-tenant change exists to make impossible.
func TestTwoTenantsSignInAtTwoIssuersInOneProcess(t *testing.T) {
	acmeIDP := authtest.NewIssuer(t)
	globexIDP := authtest.NewIssuer(t)
	if acmeIDP.URL == globexIDP.URL {
		t.Fatal("the two test issuers are one server")
	}
	router, conn := mountTwo(t, mapProviders{
		acme.ID: {Issuer: acmeIDP.URL, ClientID: "platformkit", SecretRef: "ACME_SECRET",
			RedirectPath: "/api/v1/auth/oidc/callback"},
		globex.ID: {Issuer: globexIDP.URL, ClientID: "platformkit", SecretRef: "GLOBEX_SECRET",
			RedirectPath: "/api/v1/auth/oidc/callback"},
	}, mapSecrets{"ACME_SECRET": "acme-secret", "GLOBEX_SECRET": "globex-secret"})

	person(t, conn, "ada@acme.example.com", contracts.RoleMember)

	acmeChallenge, acmeState, acmeCookie, _ := start(t, router, host)
	globexChallenge, globexState, globexCookie, _ := start(t, router, secondHost)
	if acmeChallenge == globexChallenge {
		t.Fatal("both tenants were sent the same PKCE challenge")
	}

	// Each tenant's redirect names its own issuer and nobody else's.
	for _, tt := range []struct {
		at, want string
	}{
		{host, acmeIDP.URL},
		{secondHost, globexIDP.URL},
	} {
		res := get(t, router, tt.at, "/api/v1/auth/oidc/start")
		if res.Code != http.StatusSeeOther {
			t.Fatalf("%s start = %d %s, want 303", tt.at, res.Code, res.Body.String())
		}
		to, err := url.Parse(res.Header().Get("Location"))
		if err != nil {
			t.Fatalf("%s redirect: %v", tt.at, err)
		}
		if !strings.HasPrefix(to.String(), tt.want) {
			t.Errorf("%s sends its people to %s, want %s", tt.at, to, tt.want)
		}
	}

	// The round trip, at acme, against acme's provider.
	acmeIDP.Issue("acme-code", "ada@acme.example.com", true, "platformkit", nonce(acmeCookie))
	res := callback(t, router, host, "acme-code", acmeState, acmeCookie)
	if res.Code != http.StatusSeeOther {
		t.Fatalf("acme's callback = %d %s, want 303 to /", res.Code, res.Body.String())
	}
	if sessionCookie(res) == "" {
		t.Fatal("acme's callback set no session cookie")
	}

	// The same address, the same browser, globex's host — and globex has no
	// account for her. It is a refusal, not a session: the identity provider that
	// vouches for an address is acme's, and the row that would be found is in a
	// tenant whose host this is not.
	globexIDP.Issue("globex-code", "ada@acme.example.com", true, "platformkit", nonce(globexCookie))
	if res := callback(t, router, secondHost, "globex-code", globexState, globexCookie); res.Code != http.StatusForbidden {
		t.Errorf("an address globex does not have signed in with %d; want 403", res.Code)
	}

	// And acme's code is worth nothing at globex's callback: the provider that
	// minted it is not the one globex exchanges with.
	if res := callback(t, router, secondHost, "acme-code", globexState, acmeCookie); res.Code == http.StatusSeeOther {
		t.Errorf("acme's code completed a session at globex; the discovery cache is not keyed by issuer")
	}
}

// TestATenantWithNoProviderIsRefusedBeforeTheProviderIsDialled covers the two
// answers a tenant without single sign-on gets: no row at all is a 404, and the
// installation's own issuer is the fallback when there is one.
func TestATenantWithNoProviderIsRefusedBeforeTheProviderIsDialled(t *testing.T) {
	idp := authtest.NewIssuer(t)
	calls := 0
	dialled := idp.Server.Config.Handler
	idp.Server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		dialled.ServeHTTP(w, r)
	})
	router, _ := mountTwo(t, mapProviders{}, mapSecrets{})
	if res := get(t, router, host, "/api/v1/auth/oidc/start"); res.Code != http.StatusNotFound {
		t.Errorf("a tenant with no provider = %d %s, want 404", res.Code, res.Body.String())
	}
	if calls != 0 {
		t.Errorf("the refusal asked the provider for %d things; a 404 should dial nothing", calls)
	}

	// A reference the deployment never resolved is the tenant's provider failing
	// to exist right now: 503, and it wrote nothing.
	router, _ = mountTwo(t, mapProviders{acme.ID: {Issuer: idp.URL, ClientID: "platformkit",
		SecretRef: "NOT_SET_ANYWHERE", RedirectPath: "/api/v1/auth/oidc/callback"}}, mapSecrets{})
	if res := get(t, router, host, "/api/v1/auth/oidc/start"); res.Code != http.StatusServiceUnavailable {
		t.Errorf("a secret reference with no secret behind it = %d, want 503", res.Code)
	}
}

// nonce is what the test issuer has to put back in the id token, read out of the
// state cookie the start set: state.nonce.verifier, which is what went out.
func nonce(cookie string) string {
	parts := strings.Split(cookie, ".")
	if len(parts) != 3 {
		return ""
	}
	return parts[1]
}
