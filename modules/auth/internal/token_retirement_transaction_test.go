package internal_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/internal"
)

func TestSelfRevocationRollsBackTheKeyAndItsEventTogether(t *testing.T) {
	router, conn, svc := mount(t, auth.OIDC{})
	person(t, conn, "ada@acme.localhost", contracts.RoleAdmin)
	session := signIn(t, router, "ada@acme.localhost")
	key, _ := mintKey(t, router, session, "Phone")
	tokens, ok := svc.(contracts.Tokens)
	if !ok {
		t.Fatal("the auth service provides no token commands")
	}
	ctx := tenancy.WithTenant(t.Context(), acme)
	err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		req := httptest.NewRequest(http.MethodPost, "http://"+host+revokeSelf, nil)
		bearer(key)(req)
		caller, ok, err := svc.Authenticate(ctx, tx, req)
		if err != nil || !ok {
			t.Fatalf("authenticate live key: recognised=%v err=%v", ok, err)
		}
		if err := tokens.RevokeOwnToken(ctx, tx, caller); err != nil {
			t.Fatalf("self-revoke: %v", err)
		}
		var count int64
		if err := tx.DB().Table("platformkit_outbox").Where("name = ?", contracts.EventAPITokenRevoked).Count(&count).Error; err != nil || count != 1 {
			t.Fatalf("uncommitted revocation events = %d (%v), want 1", count, err)
		}
		return errRollback
	})
	if !errors.Is(err, errRollback) {
		t.Fatalf("rollback = %v", err)
	}
	if res := call(t, router, http.MethodGet, "/api/v1/auth/roles", "", bearer(key)); res.Code != http.StatusOK {
		t.Errorf("rolled-back self-revoke ended the key: %d %s", res.Code, res.Body.String())
	}
	err = db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var count int64
		if err := tx.DB().Table("platformkit_outbox").Where("name = ?", contracts.EventAPITokenRevoked).Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			t.Errorf("rolled-back self-revoke committed %d events", count)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestATokenSelfRevocationCannotReadOrWriteAnotherTenantsKey(t *testing.T) {
	router, conn := mountTwo(t, mapProviders{}, mapSecrets{})
	ada := person(t, conn, "ada@acme.localhost", contracts.RoleAdmin)
	key, id := mintKey(t, router, signIn(t, router, "ada@acme.localhost"), "Phone")
	svc := internal.NewService(realUsers(), nil, internal.Delivery{})
	ctx := tenancy.WithTenant(t.Context(), globex)
	err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		before := outbox(t, tx)
		rows, err := svc.ListTokens(ctx, tx, ada)
		if err != nil || len(rows) != 0 {
			t.Fatalf("foreign tenant reads %d keys (%v), want none", len(rows), err)
		}
		err = svc.RevokeOwnToken(ctx, tx, tenancy.Principal{UserID: ada, TokenID: uuid.MustParse(id)})
		if !errors.Is(err, crud.ErrNotFound) {
			t.Errorf("foreign tenant's self-revoke = %v, want not found", err)
		}
		if after := outbox(t, tx); !slices.Equal(before, after) {
			t.Errorf("refused self-revoke changed outbox: %v -> %v", before, after)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if res := call(t, router, http.MethodGet, "/api/v1/auth/roles", "", bearer(key)); res.Code != http.StatusOK {
		t.Errorf("another tenant ended the key: %d %s", res.Code, res.Body.String())
	}
}

func TestSelfRevocationRefusesAChangedExpectedPrincipalWithoutEndingTheKey(t *testing.T) {
	router, conn, _ := mount(t, auth.OIDC{})
	person(t, conn, "ada@acme.localhost", contracts.RoleAdmin)
	key, _ := mintKey(t, router, signIn(t, router, "ada@acme.localhost"), "Phone")
	res := call(t, router, http.MethodPost, revokeSelf, "", bearer(key), func(req *http.Request) {
		req.Header.Set(httpx.ExpectedPrincipalHeader, uuid.NewString())
	})
	if res.Code != http.StatusForbidden || !strings.Contains(res.Body.String(), httpx.CodePrincipalChanged) {
		t.Errorf("stale principal self-revoke = %d %s, want 403 %s", res.Code, res.Body.String(), httpx.CodePrincipalChanged)
	}
	if res := call(t, router, http.MethodGet, "/api/v1/auth/roles", "", bearer(key)); res.Code != http.StatusOK {
		t.Errorf("a refused self-revoke ended the key: %d %s", res.Code, res.Body.String())
	}
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var count int64
		if err := tx.DB().Table("platformkit_outbox").Where("name = ?", contracts.EventAPITokenRevoked).Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			t.Errorf("refused self-revoke committed %d revocation events", count)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestATokenCannotSelectItsHoldersOtherKeyForSelfRevocation(t *testing.T) {
	router, conn, _ := mount(t, auth.OIDC{})
	person(t, conn, "ada@acme.localhost", contracts.RoleAdmin)
	session := signIn(t, router, "ada@acme.localhost")
	phone, _ := mintKey(t, router, session, "Phone")
	laptop, laptopID := mintKey(t, router, session, "Laptop")
	res := call(t, router, http.MethodPost, revokeSelf+"?id="+laptopID,
		`{"id":"`+laptopID+`","tokenID":"`+laptopID+`"}`, bearer(phone))
	if res.Code != http.StatusNoContent && res.Code != http.StatusForbidden {
		t.Fatalf("self-revoke with another key as input = %d %s, want 204 or 403", res.Code, res.Body.String())
	}
	if other := call(t, router, http.MethodGet, "/api/v1/auth/roles", "", bearer(laptop)); other.Code != http.StatusOK {
		t.Errorf("a token selected its holder's other key: %d %s", other.Code, other.Body.String())
	}
	var wantEvents int64
	if res.Code == http.StatusNoContent {
		wantEvents = 1
		if res.Body.Len() != 0 {
			t.Errorf("successful self-revocation returned a stale body: %s", res.Body.String())
		}
	} else if own := call(t, router, http.MethodGet, "/api/v1/auth/roles", "", bearer(phone)); own.Code != http.StatusOK {
		t.Errorf("refused self-revocation ended its caller's key: %d", own.Code)
	}
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var count int64
		if err := tx.DB().Table("platformkit_outbox").Where("name = ?", contracts.EventAPITokenRevoked).Count(&count).Error; err != nil {
			return err
		}
		if count != wantEvents {
			t.Errorf("self-revocation committed %d revocation events, want %d", count, wantEvents)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
