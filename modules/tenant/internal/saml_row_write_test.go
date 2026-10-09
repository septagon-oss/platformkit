package internal_test

// Which identity provider vouches for a company's people is chosen by the control
// plane and by nobody inside the company: a tenant transaction that tries to point
// its own row, or another tenant's, at an IdP of its choosing writes nothing, and
// the sign-in's read afterwards still answers the provider the operator set.

import (
	"context"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts/tenanttest"
	"github.com/septagon-oss/platformkit/modules/tenant/internal"
	"github.com/septagon-oss/platformkit/modules/user"
)

func TestATenantTransactionCannotRepointASAMLProvider(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, auth.Migrations)
	acme, globex := acmeSAML(t, conn)
	svc := internal.NewService(nil, tenanttest.InstallationLanguages(), "")

	for _, from := range []*contracts.Tenant{acme, globex} {
		_ = db.Run(tenancy.WithTenant(t.Context(), from.Tenancy()), conn,
			func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				moved := tx.DB().Exec(`UPDATE tenants SET saml_entity_id = 'urn:evil',
					saml_metadata_url = 'https://idp.evil.example/metadata', saml_metadata_xml = NULL,
					saml_registration = 'provision', saml_roles = '{admin}' WHERE id = ?`, acme.ID)
				if moved.Error == nil && moved.RowsAffected != 0 {
					t.Errorf("%s's transaction repointed acme's SAML provider (%d rows)", from.Slug, moved.RowsAffected)
				}
				return nil
			})
	}

	err := db.Run(tenancy.WithTenant(t.Context(), acme.Tenancy()), conn,
		func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			got, ok, err := svc.SAMLSettingsOf(ctx, tx)
			if err != nil {
				return err
			}
			if !ok || got.EntityID != acmeFederation.EntityID || got.MetadataURL != acmeFederation.MetadataURL ||
				got.MetadataXML != idpMetadata || got.RegistrationMode() != contracts.RegistrationExisting {
				t.Errorf("acme's sign-in now reads %+v (ok=%v), want the provider the operator set", got, ok)
			}
			return nil
		})
	if err != nil {
		t.Fatalf("acme's read: %v", err)
	}
}
