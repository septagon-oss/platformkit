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

func TestReplayedLoginStillSignsTheClientIn(t *testing.T) {
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
	key := func(r *http.Request) { r.Header.Set(httpx.IdempotencyKeyHeader, "login-once") }
	first := call(t, router, http.MethodPost, "/api/v1/auth/login", body, key)
	if first.Code != http.StatusOK {
		t.Fatalf("first login = %d %s", first.Code, first.Body.String())
	}
	// Model a dropped first response: the client never received its cookie.
	retry := call(t, router, http.MethodPost, "/api/v1/auth/login", body, key)
	if retry.Code != http.StatusOK {
		t.Fatalf("retried login = %d %s", retry.Code, retry.Body.String())
	}
	if retry.Body.String() != first.Body.String() {
		t.Error("retried login did not replay the original answer")
	}
	me := call(t, router, http.MethodGet, "/api/v1/auth/me", "", withSession(sessionCookie(retry)))
	if me.Code != http.StatusOK {
		t.Errorf("replayed login returned %d with %d cookies, but the client remains unauthenticated: GET /me = %d; want a usable sign-in", retry.Code, len(retry.Result().Cookies()), me.Code)
	}
}
