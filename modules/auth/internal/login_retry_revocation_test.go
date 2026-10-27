package internal_test

import (
	"log/slog"
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/idempotency"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	"github.com/septagon-oss/platformkit/modules/auth/internal"
)

func TestLoginRetryAfterRevocationNeverAnswersWithAnUnusableSession(t *testing.T) {
	_, conn, svc := mount(t, auth.OIDC{})
	person(t, conn, "ada@acme.localhost", contracts.RoleAdmin)
	api, router := httpx.New(httpx.Options{
		Cache: cache.Memory("pkit"), PublicHost: host, Tenants: site{}, Conn: conn,
		Authorize: svc, Authenticate: svc.Authenticate,
		Idempotency: idempotency.Postgres(httpx.ConnFrom),
		Log:         slog.New(slog.DiscardHandler),
	})
	internal.RegisterRoutes(surfacesOf(api), internal.RouteServices{Login: svc, Logout: svc, Identity: svc, ChangePassword: svc, ForgotPassword: svc, ResetPassword: svc, ListSessions: svc, RevokeSession: svc, RevokeAllSessions: svc, ListRoles: svc, SetRole: svc}, internal.NewCookies(false))
	body := `{"email":"ada@acme.localhost","password":"` + authtest.Password + `"}`
	key := func(r *http.Request) { r.Header.Set(httpx.IdempotencyKeyHeader, "revoked-login") }
	first := call(t, router, http.MethodPost, "/api/v1/auth/login", body, key)
	if first.Code != http.StatusOK {
		t.Fatalf("first login = %d %s", first.Code, first.Body.String())
	}
	session := sessionCookie(first)
	if me := call(t, router, http.MethodGet, "/api/v1/auth/me", "", withSession(session)); me.Code != http.StatusOK {
		t.Fatalf("first session did not authenticate: %d", me.Code)
	}
	logout := call(t, router, http.MethodPost, "/api/v1/auth/logout", "", withSession(session),
		func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "same-origin") })
	if logout.Code != http.StatusOK {
		t.Fatalf("logout = %d %s", logout.Code, logout.Body.String())
	}
	if me := call(t, router, http.MethodGet, "/api/v1/auth/me", "", withSession(session)); me.Code != http.StatusUnauthorized {
		t.Fatalf("revoked session still authenticated: %d", me.Code)
	}
	retry := call(t, router, http.MethodPost, "/api/v1/auth/login", body, key)
	if retry.Code >= 400 && retry.Code < 500 {
		return
	}
	if retry.Code != http.StatusOK {
		t.Fatalf("retry = %d %s; want usable sign-in or explicit refusal", retry.Code, retry.Body.String())
	}
	if me := call(t, router, http.MethodGet, "/api/v1/auth/me", "", withSession(sessionCookie(retry))); me.Code != http.StatusOK {
		t.Errorf("retry reported success but its session did not authenticate: %d", me.Code)
	}
}
