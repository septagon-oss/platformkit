package internal_test

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/idempotency"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/internal"
)

func TestAKeyedSelfRevocationNeverConfirmsALiveKeyIsRetired(t *testing.T) {
	original, conn, svc := mount(t, auth.OIDC{})
	person(t, conn, "ada@acme.localhost", contracts.RoleAdmin)
	session := signIn(t, original, "ada@acme.localhost")
	phone, _ := mintKey(t, original, session, "Phone")
	laptop, _ := mintKey(t, original, session, "Laptop")
	api, router := httpx.New(httpx.Options{
		Cache: cache.Memory("pkit"), PublicHost: host, Tenants: site{}, Conn: conn,
		Authorize: svc, Authenticate: svc.Authenticate,
		Idempotency: idempotency.Postgres(httpx.ConnFrom),
		Log:         slog.New(slog.DiscardHandler),
	})
	tokens, ok := svc.(contracts.Tokens)
	if !ok {
		t.Fatal("the auth service provides no token commands")
	}
	internal.RegisterTokenRoutes(surfacesOf(api), tokens)
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatal(err)
	}
	key := func(r *http.Request) { r.Header.Set(httpx.IdempotencyKeyHeader, "retire-device") }
	first := call(t, router, http.MethodPost, revokeSelf, "", bearer(phone), key)
	if first.Code != http.StatusNoContent || first.Body.Len() != 0 {
		t.Fatalf("phone retirement = %d %s, want empty 204", first.Code, first.Body.String())
	}
	retry := call(t, router, http.MethodPost, revokeSelf, "", bearer(phone), key)
	if retry.Code != http.StatusUnauthorized || !strings.HasPrefix(retry.Header().Get("WWW-Authenticate"), "Bearer") {
		t.Errorf("retired phone retry = %d %s, want 401 with Bearer challenge", retry.Code, retry.Body.String())
	}
	second := call(t, router, http.MethodPost, revokeSelf, "", bearer(laptop), key)
	wantEvents := int64(1)
	switch second.Code {
	case http.StatusNoContent:
		wantEvents = 2
		if second.Body.Len() != 0 {
			t.Errorf("laptop retirement returned a stale body: %s", second.Body.String())
		}
		if res := call(t, original, http.MethodGet, "/api/v1/auth/roles", "", bearer(laptop)); res.Code != http.StatusForbidden {
			t.Errorf("laptop was told 204 but its key still authenticates: GET roles = %d %s, want 403", res.Code, res.Body.String())
		}
	case http.StatusConflict, http.StatusUnprocessableEntity:
		if second.Body.Len() == 0 {
			t.Error("refused reuse of another key's retirement must name a reason")
		}
	default:
		t.Errorf("laptop retirement = %d %s, want successful retirement or an explicit key conflict", second.Code, second.Body.String())
	}
	if err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var count int64
		if err := tx.DB().Table("platformkit_outbox").Where("name = ?", contracts.EventAPITokenRevoked).Count(&count).Error; err != nil {
			return err
		}
		if count != wantEvents {
			t.Errorf("successful retirements emitted %d events, want %d", count, wantEvents)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
