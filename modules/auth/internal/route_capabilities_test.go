package internal_test

import (
	"context"
	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	"github.com/septagon-oss/platformkit/modules/auth/internal"
	"log/slog"
	"net/http"
	"strings"
	"testing"
)

type narrowLogin struct {
	next  contracts.PasswordLogin
	calls map[string]int
}

func (n narrowLogin) Precheck(ctx context.Context, email, ip string) contracts.Verdict {
	n.calls["Precheck"]++
	return n.next.Precheck(ctx, email, ip)
}
func (n narrowLogin) Login(ctx context.Context, tx db.Tx[db.Tenant], email, password string, from contracts.Client) (*contracts.Session, *contracts.Identity, error) {
	n.calls["Login"]++
	return n.next.Login(ctx, tx, email, password, from)
}

type narrowLogout struct {
	next  contracts.SessionEnder
	calls map[string]int
}

func (n narrowLogout) Logout(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) error {
	n.calls["Logout"]++
	return n.next.Logout(ctx, tx, id)
}

type narrowIdentity struct {
	next  contracts.IdentityReader
	calls map[string]int
}

func (n narrowIdentity) Identify(ctx context.Context, tx db.Tx[db.Tenant], session uuid.UUID, from contracts.Client) (*contracts.Identity, error) {
	n.calls["Identify"]++
	return n.next.Identify(ctx, tx, session, from)
}

type narrowChangePassword struct {
	next  contracts.PasswordChanger
	calls map[string]int
}

func (n narrowChangePassword) ChangePassword(ctx context.Context, tx db.Tx[db.Tenant], userID, keep uuid.UUID, current, next string) error {
	n.calls["ChangePassword"]++
	return n.next.ChangePassword(ctx, tx, userID, keep, current, next)
}

type narrowForgotPassword struct {
	next  contracts.ResetRequester
	calls map[string]int
}

func (n narrowForgotPassword) MayAsk(ctx context.Context, ip string) contracts.Decision {
	n.calls["MayAsk"]++
	return n.next.MayAsk(ctx, ip)
}
func (n narrowForgotPassword) Forget(ctx context.Context, tx db.Tx[db.Tenant], email string) error {
	n.calls["Forget"]++
	return n.next.Forget(ctx, tx, email)
}

type narrowResetPassword struct {
	next  contracts.PasswordResetter
	calls map[string]int
}

func (n narrowResetPassword) MayRedeem(ctx context.Context, ip string) contracts.Decision {
	n.calls["MayRedeem"]++
	return n.next.MayRedeem(ctx, ip)
}
func (n narrowResetPassword) Reset(ctx context.Context, tx db.Tx[db.Tenant], token, password string,
	from contracts.Client) (*contracts.Session, error) {
	n.calls["Reset"]++
	return n.next.Reset(ctx, tx, token, password, from)
}

type narrowListSessions struct {
	next  contracts.SessionReader
	calls map[string]int
}

func (n narrowListSessions) Sessions(ctx context.Context, tx db.Tx[db.Tenant], userID, current uuid.UUID) ([]*contracts.SessionListing, error) {
	n.calls["Sessions"]++
	return n.next.Sessions(ctx, tx, userID, current)
}

type narrowRevokeSession struct {
	next  contracts.SessionRevoker
	calls map[string]int
}

func (n narrowRevokeSession) RevokeSession(ctx context.Context, tx db.Tx[db.Tenant], userID uuid.UUID, ref string) error {
	n.calls["RevokeSession"]++
	return n.next.RevokeSession(ctx, tx, userID, ref)
}

type narrowRevokeAllSessions struct {
	next  contracts.AllSessionsRevoker
	calls map[string]int
}

func (n narrowRevokeAllSessions) RevokeAllSessions(ctx context.Context, tx db.Tx[db.Tenant], userID uuid.UUID) (int, error) {
	n.calls["RevokeAllSessions"]++
	return n.next.RevokeAllSessions(ctx, tx, userID)
}

type narrowListRoles struct {
	next  contracts.RoleReader
	calls map[string]int
}

func (n narrowListRoles) Roles(ctx context.Context, tx db.Tx[db.Tenant]) ([]*contracts.Role, error) {
	n.calls["Roles"]++
	return n.next.Roles(ctx, tx)
}

type narrowSetRole struct {
	next  contracts.RoleWriter
	calls map[string]int
}

func (n narrowSetRole) SetRole(ctx context.Context, tx db.Tx[db.Tenant], name string, permissions []string, declared []tenancy.Grant) (*contracts.Role, error) {
	n.calls["SetRole"]++
	return n.next.SetRole(ctx, tx, name, permissions, declared)
}

func TestAuthRoutesUseDistinctNarrowCapabilities(t *testing.T) {
	_, conn, svc := mount(t, auth.OIDC{})
	person(t, conn, "ada@acme.localhost", contracts.RoleAdmin)
	api, router := httpx.New(httpx.Options{
		Cache: cache.Memory("pkit"), PublicHost: host, Tenants: site{}, Conn: conn,
		Authorize: svc, Authenticate: svc.Authenticate, Log: slog.New(slog.DiscardHandler),
	})
	calls := map[string]int{}
	internal.RegisterRoutes(surfacesOf(api), internal.RouteServices{
		Login:             narrowLogin{next: svc, calls: calls},
		Logout:            narrowLogout{next: svc, calls: calls},
		Identity:          narrowIdentity{next: svc, calls: calls},
		ChangePassword:    narrowChangePassword{next: svc, calls: calls},
		ForgotPassword:    narrowForgotPassword{next: svc, calls: calls},
		ResetPassword:     narrowResetPassword{next: svc, calls: calls},
		ListSessions:      narrowListSessions{next: svc, calls: calls},
		RevokeSession:     narrowRevokeSession{next: svc, calls: calls},
		RevokeAllSessions: narrowRevokeAllSessions{next: svc, calls: calls},
		ListRoles:         narrowListRoles{next: svc, calls: calls},
		SetRole:           narrowSetRole{next: svc, calls: calls},
	}, internal.NewCookies(false))
	login := call(t, router, http.MethodPost, "/api/v1/auth/login", `{"email":"ada@acme.localhost","password":"`+authtest.Password+`"}`)
	if login.Code != http.StatusOK {
		t.Fatalf("login = %d %s", login.Code, login.Body)
	}
	cookie := sessionCookie(login)
	for _, tc := range []struct {
		name, method, path, body string
		status                   int
		called                   []string
	}{
		{"identity", "GET", "/api/v1/auth/me", "", 200, []string{"Identify"}},
		{"sessions", "GET", "/api/v1/auth/sessions", "", 200, []string{"Sessions"}},
		{"roles", "GET", "/api/v1/auth/roles", "", 200, []string{"Roles"}},
		{"role write", "PUT", "/api/v1/auth/roles/viewer", `{"permissions":[]}`, 200, []string{"SetRole"}},
		{"password", "POST", "/api/v1/auth/password", `{"current":"` + authtest.Password + `","new":"a sufficiently long replacement"}`, 200, []string{"ChangePassword"}},
		{"forgot", "POST", "/api/v1/public/auth/password/forgot", `{"email":"nobody@example.test"}`, 200, []string{"MayAsk", "Forget"}},
		{"invalid reset", "POST", "/api/v1/auth/password/reset", `{"token":"invalid","new":"a sufficiently long replacement"}`, 401, []string{"MayRedeem", "Reset"}},
		{"unknown session", "POST", "/api/v1/auth/sessions/" + strings.Repeat("0", 64) + "/revoke", "{}", 404, []string{"RevokeSession"}},
		{"logout", "POST", "/api/v1/auth/logout", "{}", 200, []string{"Logout"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			answer := call(t, router, tc.method, tc.path, tc.body, withSession(cookie))
			if answer.Code != tc.status {
				t.Fatalf("%s = %d %s", tc.path, answer.Code, answer.Body)
			}
			for _, method := range tc.called {
				if calls[method] == 0 {
					t.Errorf("%s capability was not called", method)
				}
			}
		})
	}
	login = call(t, router, http.MethodPost, "/api/v1/auth/login", `{"email":"ada@acme.localhost","password":"a sufficiently long replacement"}`)
	if login.Code != 200 {
		t.Fatalf("second login = %d %s", login.Code, login.Body)
	}
	answer := call(t, router, "POST", "/api/v1/auth/sessions/revoke-all", "{}", withSession(sessionCookie(login)))
	if answer.Code != 200 || calls["RevokeAllSessions"] != 1 || !strings.Contains(answer.Body.String(), "signedOut") {
		t.Fatalf("all sessions = %d %s", answer.Code, answer.Body)
	}
	if calls["Precheck"] == 0 || calls["Login"] == 0 {
		t.Fatal("password login capabilities were not called")
	}
}
