package main

import (
	"context"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts/tenanttest"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

type installationBootstrap struct{ tenantcontracts.Bootstrapper }
type installationLocale func(context.Context, db.Tx[db.System], uuid.UUID, tenantcontracts.SetLocale) (*tenantcontracts.Tenant, error)

func (f installationLocale) SetLocale(ctx context.Context, tx db.Tx[db.System], id uuid.UUID, in tenantcontracts.SetLocale) (*tenantcontracts.Tenant, error) {
	return f(ctx, tx, id, in)
}

type installationAccount func(context.Context, db.Tx[db.System], uuid.UUID, string, string, string, []string) (*usercontracts.User, error)

func (f installationAccount) Provision(ctx context.Context, tx db.Tx[db.System], id uuid.UUID, email, name, password string, roles []string) (*usercontracts.User, error) {
	return f(ctx, tx, id, email, name, password, roles)
}

func TestBootstrapAndLocalStartUseOnlyInstallationCapabilities(t *testing.T) {
	_, conn := dbtest.Schema(t)
	for _, languages := range [][]string{nil, {"en", "pt-PT"}} {
		fake := tenanttest.NewFake()
		calls := map[string]int{}
		var first uuid.UUID
		err := dbtest.System(t.Context(), conn, func(ctx context.Context, system db.Tx[db.System]) error {
			c := composition{bootstrap: installationBootstrap{fake},
				provisioner: installationAccount(func(_ context.Context, tx db.Tx[db.System], id uuid.UUID, email, name, password string, roles []string) (*usercontracts.User, error) {
					calls["account"]++
					first = id
					if tx.DB() != system.DB() || email != "admin@example.test" || name != "" || password != "fixture password" || !slices.Equal(roles, []string{authcontracts.RoleAdmin}) {
						t.Fatal("bootstrap changed account input or transaction")
					}
					return &usercontracts.User{}, nil
				}),
				locale: installationLocale(func(_ context.Context, tx db.Tx[db.System], id uuid.UUID, in tenantcontracts.SetLocale) (*tenantcontracts.Tenant, error) {
					calls["locale"]++
					if tx.DB() != system.DB() || id != first || !slices.Equal(in.Supported, languages) {
						t.Fatal("bootstrap changed locale input or transaction")
					}
					return &tenantcontracts.Tenant{}, nil
				}),
			}
			return c.bootstrapIdentity(ctx, system, tenantcontracts.NewTenant{Slug: "installation", Name: "Installation", Host: "ops.example.test"}, "admin@example.test", "fixture password", languages)
		})
		if err != nil {
			t.Fatal(err)
		}
		if first == uuid.Nil || calls["account"] != 1 || (calls["locale"] == 1) != (len(languages) > 0) {
			t.Fatalf("bootstrap calls = %v", calls)
		}
	}
}
