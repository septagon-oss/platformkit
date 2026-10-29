package internal_test

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	"github.com/septagon-oss/platformkit/modules/auth/internal"
	"github.com/septagon-oss/platformkit/modules/user"
	"github.com/septagon-oss/platformkit/modules/user/contracts/usertest"
)

type sessionOpener func(context.Context, db.Tx[db.Tenant], uuid.UUID, contracts.Client, contracts.SignInMethod) (*contracts.Session, *contracts.Identity, error)

func (f sessionOpener) Open(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, from contracts.Client, method contracts.SignInMethod) (*contracts.Session, *contracts.Identity, error) {
	return f(ctx, tx, id, from, method)
}

type oidcAccess struct{}

func (oidcAccess) Allowed(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) {
	return true, nil
}

func TestOIDCCallbackNeedsOnlySessionOpener(t *testing.T) {
	issuer := authtest.NewIssuer(t)
	_, conn := dbtest.Schema(t, user.Migrations, auth.Migrations)
	users := usertest.NewFake()
	person, err := users.Invite(t.Context(), db.Tx[db.Tenant]{}, "ada@example.test", "Ada")
	if err != nil {
		t.Fatal(err)
	}
	if err := users.SetPassword(t.Context(), db.Tx[db.Tenant]{}, person.ID, "a sufficiently long password"); err != nil {
		t.Fatal(err)
	}
	api, router := httpx.New(httpx.Options{Conn: conn, Cache: cache.Memory("pkit"), PublicHost: host, Tenants: site{}, Authorize: oidcAccess{},
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{}, false, nil
		}})
	calls := 0
	opener := sessionOpener(func(_ context.Context, tx db.Tx[db.Tenant], id uuid.UUID, _ contracts.Client, method contracts.SignInMethod) (*contracts.Session, *contracts.Identity, error) {
		calls++
		if id != person.ID || db.TenantOf(tx).ID != acme.ID || method != contracts.ViaOIDC {
			t.Fatal("callback lost account or tenant")
		}
		return &contracts.Session{ID: uuid.New(), UserID: id}, nil, nil
	})
	provider := internal.NewProvider(auth.OIDC{Issuer: issuer.URL, ClientID: "platformkit", ClientSecret: "secret", RedirectPath: "/api/v1/auth/oidc/callback"}, internal.Cookies{}, false, nil, nil)
	internal.RegisterOIDCRoutes(surfacesOf(api), opener, users, nil, provider)
	begin := get(t, router, host, "/api/v1/auth/oidc/start")
	if begin.Code != http.StatusSeeOther {
		t.Fatalf("start = %d %s", begin.Code, begin.Body.String())
	}
	location, err := url.Parse(begin.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	var cookie string
	for _, c := range begin.Result().Cookies() {
		if c.Name == "platformkit_oidc" {
			cookie = c.Value
		}
	}
	issuer.Issue("narrow-code", person.Email, true, "platformkit", location.Query().Get("nonce"))
	finish := callback(t, router, host, "narrow-code", location.Query().Get("state"), cookie)
	if finish.Code != http.StatusSeeOther || calls != 1 {
		t.Fatalf("callback = %d %s; calls=%d", finish.Code, finish.Body.String(), calls)
	}
}
