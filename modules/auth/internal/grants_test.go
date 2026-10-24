package internal_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	"github.com/septagon-oss/platformkit/modules/auth/internal"
	"github.com/septagon-oss/platformkit/modules/user"
)

func TestIndependentSQLGrantsConform(t *testing.T) {
	authtest.RunGrants(t, func(t *testing.T, run func(authtest.GrantFixture)) {
		_, conn := dbtest.Schema(t, user.Migrations, auth.Migrations)
		seed(t, conn, acme)
		err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			run(authtest.GrantFixture{
				Ctx: ctx, Tx: tx, Policy: internal.GrantReader{}, Published: func() []string { return outbox(t, tx) },
				Role: func(name string, permissions ...string) {
					if err := tx.DB().Exec("INSERT INTO roles (tenant_id, name, permissions) VALUES (?, ?, ?) ON CONFLICT (tenant_id, name) DO UPDATE SET permissions = EXCLUDED.permissions", acme.ID, name, pq.StringArray(permissions)).Error; err != nil {
						t.Fatal(err)
					}
				},
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})
}

func TestIndependentGrantsUseTheExplicitTenantTransaction(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, auth.Migrations)
	seed(t, conn, acme)
	seed(t, conn, globex)
	reader := internal.GrantReader{}
	principal := tenancy.Principal{UserID: uuid.New(), Roles: []string{"scope"}}
	want := tenancy.Grant{Permission: "widget:read"}
	for _, tenant := range []tenancy.Tenant{acme, globex} {
		err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
			permissions := []string{"widget:write"}
			if tenant.ID == acme.ID {
				permissions = []string{want.Permission}
			}
			if err := tx.DB().Exec("INSERT INTO roles (tenant_id, name, permissions) VALUES (?, 'scope', ?)", tenant.ID, pq.StringArray(permissions)).Error; err != nil {
				return err
			}
			ctx := tenancy.WithPrincipal(t.Context(), principal)
			if ok, err := reader.Allowed(ctx, tx, want); err != nil || ok != (tenant.ID == acme.ID) {
				t.Fatalf("tenant %s: %v, %v", tenant.Slug, ok, err)
			}
			wrong := tenancy.WithTenant(ctx, tenancy.Tenant{ID: uuid.New()})
			if ok, err := reader.Allowed(wrong, tx, want); ok || err == nil {
				t.Fatal("mismatched context tenant accepted")
			}
			operator := tenancy.WithPrincipal(ctx, tenancy.Principal{UserID: principal.UserID, Permissions: []string{"fleet:manage"}})
			if ok, err := reader.Allowed(operator, tx, tenancy.Grant{Permission: "fleet:manage", Operator: true}); ok || err != nil {
				t.Fatalf("customer operator grant: %v, %v", ok, err)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if ok, err := reader.Allowed(t.Context(), db.Tx[db.Tenant]{}, want); ok || err == nil {
		t.Fatal("zero transaction accepted")
	}
	if _, err := reader.Permissions(t.Context(), db.Tx[db.Tenant]{}, []string{"scope"}); err == nil {
		t.Fatal("role read accepted zero transaction")
	}
	if held, err := reader.Permissions(t.Context(), db.Tx[db.Tenant]{}, nil); err != nil || len(held) != 0 {
		t.Fatalf("empty role read: %v, %v", held, err)
	}
}

type failedPermissions struct{ err error }

func (p failedPermissions) Permissions(context.Context, db.Tx[db.Tenant], []string) ([]string, error) {
	return nil, p.err
}

type failedAuthorization struct{ err error }

func (p failedAuthorization) Allowed(context.Context, db.Tx[db.Tenant], tenancy.Grant) (bool, error) {
	return false, p.err
}

func TestSessionPermissionsDelegateToTheSuppliedReader(t *testing.T) {
	want := errors.New("injected role read")
	svc := internal.NewServiceWithGrants(nil, nil, internal.Delivery{}, failedAuthorization{want}, failedPermissions{want})
	if _, err := svc.Permissions(t.Context(), db.Tx[db.Tenant]{}, []string{"editor"}); !errors.Is(err, want) {
		t.Fatalf("permission delegate: %v", err)
	}
}

func TestLiveAuthorizationDelegatesToTheSuppliedPolicy(t *testing.T) {
	_, conn := dbtest.Schema(t)
	want := errors.New("injected authorization")
	svc := internal.NewServiceWithGrants(nil, nil, internal.Delivery{}, failedAuthorization{want}, failedPermissions{want})
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		allowed, err := svc.Allowed(ctx, acme, tenancy.Grant{Permission: "widget:read"})
		if allowed || !errors.Is(err, want) {
			t.Fatalf("authorization delegate = %v, %v", allowed, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
