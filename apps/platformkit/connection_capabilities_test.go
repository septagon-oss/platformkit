package main

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	"github.com/septagon-oss/platformkit/modules/site/contracts/sitetest"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
	"github.com/septagon-oss/platformkit/modules/user/contracts/usertest"
)

type connectionOIDC func(context.Context, db.Tx[db.Tenant]) (*tenantcontracts.OIDCSettings, bool, error)

func (f connectionOIDC) OIDCOf(ctx context.Context, tx db.Tx[db.Tenant]) (*tenantcontracts.OIDCSettings, bool, error) {
	return f(ctx, tx)
}

type connectionSAML func(context.Context, db.Tx[db.Tenant]) (*tenantcontracts.SAMLSettings, bool, error)

func (f connectionSAML) SAMLSettingsOf(ctx context.Context, tx db.Tx[db.Tenant]) (*tenantcontracts.SAMLSettings, bool, error) {
	return f(ctx, tx)
}

func TestConnectionReadsOnlySeparateProviderCapabilities(t *testing.T) {
	_, conn := dbtest.Schema(t)
	tenant := tenancy.Tenant{ID: uuid.New(), Name: "Fixture"}
	calls := map[string]int{}
	c := composition{sites: sitetest.NewFake(), passkeys: authtest.NewFake(usertest.NewFake()),
		oidcSettings: connectionOIDC(func(_ context.Context, tx db.Tx[db.Tenant]) (*tenantcontracts.OIDCSettings, bool, error) {
			calls["oidc"]++
			if db.TenantOf(tx).ID != tenant.ID {
				t.Fatal("OIDC read lost tenant")
			}
			return &tenantcontracts.OIDCSettings{Issuer: "https://issuer.example.test"}, true, nil
		}),
		samlSettings: connectionSAML(func(_ context.Context, tx db.Tx[db.Tenant]) (*tenantcontracts.SAMLSettings, bool, error) {
			calls["saml"]++
			if db.TenantOf(tx).ID != tenant.ID {
				t.Fatal("SAML read lost tenant")
			}
			return nil, false, nil
		}),
	}
	err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(ctx context.Context, _ db.Tx[db.Tenant]) error {
		face, err := c.describe(ctx)
		if err == nil && (!face.Methods.OIDC || face.Methods.SAML) {
			t.Fatalf("methods = %+v", face.Methods)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls["oidc"] != 1 || calls["saml"] != 1 {
		t.Fatalf("provider reads = %v", calls)
	}
}
