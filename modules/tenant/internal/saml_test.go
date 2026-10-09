package internal_test

// T-0292's cases for the per-tenant SAML provider, at the layer the tenant module
// owns. The OIDC file beside this one states why each of these four has to be a
// database case and not only a command-level one, and the reasons transfer whole:
// the read is answered from the transaction the `Host` resolved; the CHECK is the
// last thing between a hand-written UPDATE and a tenant whose sign-in redirects to
// a provider that will refuse it; the two column families must not be able to
// disagree with, or erase, each other; and the outbox payload is copied into the
// audit trail, so "the IdP's document is not in the trail" is only checkable as an
// exact set of JSON members.
//
// The last case runs the same input through `tenanttest.Fake` as well, because the
// fake is the second opinion the refusals are worth: a consumer testing against it
// must refuse the same half-provider, the same certificate-less metadata document,
// for the same reason, in the same words.

import (
	"context"
	"strings"
	"testing"

	"errors"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts/tenanttest"
	"github.com/septagon-oss/platformkit/modules/tenant/internal"
	"github.com/septagon-oss/platformkit/modules/user"
)

// acmeFederation is what an operator would configure for a company whose directory
// speaks SAML: this installation's identity for them, the IdP's own published
// document, and the attribute that carries the mailbox.
var acmeFederation = contracts.SAMLSettings{
	EntityID:       "urn:pkit:acme",
	MetadataURL:    "https://idp.acme.example/saml/metadata",
	EmailAttribute: "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress",
}

// idpMetadata is an IdPSSODescriptor with the three things a document has to carry
// for a sign-in to be possible against it: an SSO location on a binding the SP can
// use, and a signing certificate to check an assertion's signature against.
const idpMetadata = `<EntityDescriptor xmlns="urn:oasis:names:tc:SAML:2.0:metadata" entityID="https://idp.acme.example">
  <IDPSSODescriptor protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol">
    <KeyDescriptor use="signing">
      <KeyInfo xmlns="http://www.w3.org/2000/09/xmldsig#"><X509Data><X509Certificate>MIIBkTCB+w==</X509Certificate></X509Data></KeyInfo>
    </KeyDescriptor>
    <SingleSignOnService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-Redirect" Location="https://idp.acme.example/sso"/>
  </IDPSSODescriptor>
</EntityDescriptor>`

// acmeSAML and globex create acme with a SAML provider — both sources, so the read
// has to carry the document too — and globex with none, and hand back the pair with
// acme's OIDC provider left in place. Every claim below is about one tenant not
// seeing, or not erasing, the other's door.
func acmeSAML(t *testing.T, conn *db.Conn) (*contracts.Tenant, *contracts.Tenant) {
	t.Helper()
	svc := internal.NewService(nil, tenanttest.InstallationLanguages(), "")
	installed(t, conn, svc)
	var acme, globex *contracts.Tenant
	both := acmeFederation
	both.MetadataXML = idpMetadata
	err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		var err error
		if acme, err = svc.Create(ctx, tx, contracts.NewTenant{Slug: "acme", Name: "Acme", Host: "acme.example.com"}); err != nil {
			return err
		}
		if _, err = svc.SetOIDC(ctx, tx, acme.ID, acmePeople); err != nil {
			return err
		}
		if _, err = svc.SetSAML(ctx, tx, acme.ID, both); err != nil {
			return err
		}
		globex, err = svc.Create(ctx, tx, contracts.NewTenant{Slug: "globex", Name: "Globex", Host: "globex.example.com"})
		return err
	})
	if err != nil {
		t.Fatalf("set up two tenants: %v", err)
	}
	return acme, globex
}

// TestTwoTenantsReadTwoSAMLProvidersFromTheirOwnTransactions is the brief's claim at
// this layer: one process, two customers, two IdPs — and the tenant nobody configured
// answers false rather than an error, which is what makes its /saml/start a 404
// instead of a redirect to somebody else's federation.
func TestTwoTenantsReadTwoSAMLProvidersFromTheirOwnTransactions(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, auth.Migrations)
	acme, globex := acmeSAML(t, conn)
	svc := internal.NewService(nil, tenanttest.InstallationLanguages(), "")

	for _, want := range []struct {
		who      *contracts.Tenant
		entityID string
	}{
		{acme, acmeFederation.EntityID},
		{globex, ""},
	} {
		err := db.Run(tenancy.WithTenant(t.Context(), want.who.Tenancy()), conn,
			func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				got, ok, err := svc.SAMLSettingsOf(ctx, tx)
				if err != nil {
					return err
				}
				if want.entityID == "" {
					if ok {
						t.Errorf("%s has a SAML provider (%v) and nobody configured one; its sign-in would dial somebody else's IdP",
							want.who.Slug, got)
					}
					return nil
				}
				if !ok {
					t.Errorf("%s has no SAML provider, want %s", want.who.Slug, want.entityID)
					return nil
				}
				if got.EntityID != want.entityID || got.MetadataURL != acmeFederation.MetadataURL ||
					got.EmailAttribute != acmeFederation.EmailAttribute || got.MetadataXML != idpMetadata {
					t.Errorf("%s read back entityId=%q metadataUrl=%q emailAttribute=%q xml=%d bytes, want %q/%q/%q/%d",
						want.who.Slug, got.EntityID, got.MetadataURL, got.EmailAttribute, len(got.MetadataXML),
						want.entityID, acmeFederation.MetadataURL, acmeFederation.EmailAttribute, len(idpMetadata))
				}
				if got.Registration != contracts.RegistrationExisting {
					t.Errorf("%s registered as %q, want the default %q spelled out",
						want.who.Slug, got.Registration, contracts.RegistrationExisting)
				}
				return nil
			})
		if err != nil {
			t.Fatalf("%s's read: %v", want.who.Slug, err)
		}
	}
}

// TestHalfASAMLProviderCannotBeStored is migrations/000046's CHECK holding against a
// write that went nowhere near a command: an entity ID with neither metadata source
// is an audience nobody can verify against, and `provision` with no roles is a door
// into an empty room.
func TestHalfASAMLProviderCannotBeStored(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, auth.Migrations)
	acme, _ := acmeSAML(t, conn)

	for _, half := range []struct {
		name  string
		query string
	}{
		{"an entity ID with no metadata source",
			"UPDATE tenants SET saml_metadata_url = NULL, saml_metadata_xml = NULL WHERE id = $1"},
		{"an entity ID with no email attribute",
			"UPDATE tenants SET saml_email_attribute = NULL WHERE id = $1"},
		{"provision with no roles",
			"UPDATE tenants SET saml_registration = 'provision', saml_roles = '{}' WHERE id = $1"},
	} {
		err := dbtest.System(t.Context(), conn, func(_ context.Context, tx db.Tx[db.System]) error {
			return tx.DB().Exec(half.query, acme.ID).Error
		})
		if err == nil {
			t.Errorf("%s was stored: the CHECK in migrations/000046 does not hold", half.name)
			continue
		}
		if !strings.Contains(err.Error(), "tenants_saml_complete") {
			t.Errorf("%s was refused by %v, want the tenants_saml_complete constraint", half.name, err)
		}
	}
}

// TestSAMLAndOIDCAreIndependent is the claim the brief's "beside OIDC" turns on: a
// tenant may hold both, and neither command may touch the other's columns. A shared
// mode column, or a clear that wrote both families, would pass every single-provider
// test in this repository.
func TestSAMLAndOIDCAreIndependent(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, auth.Migrations)
	acme, _ := acmeSAML(t, conn)
	svc := internal.NewService(nil, tenanttest.InstallationLanguages(), "")

	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		_, err := svc.ClearSAML(ctx, tx, acme.ID)
		return err
	}); err != nil {
		t.Fatalf("clear acme's SAML provider: %v", err)
	}

	err := db.Run(tenancy.WithTenant(t.Context(), acme.Tenancy()), conn,
		func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			if _, ok, err := svc.SAMLSettingsOf(ctx, tx); err != nil || ok {
				t.Errorf("acme still answers a SAML provider (ok=%v err=%v) after ClearSAML", ok, err)
			}
			oidc, ok, err := svc.OIDCOf(ctx, tx)
			if err != nil || !ok {
				t.Fatalf("acme lost its OIDC provider with its SAML one (ok=%v err=%v): ClearSAML wrote columns that are not its own", ok, err)
			}
			if oidc.Issuer != acmePeople.Issuer {
				t.Errorf("acme's issuer reads back %q after ClearSAML, want %q", oidc.Issuer, acmePeople.Issuer)
			}
			return nil
		})
	if err != nil {
		t.Fatalf("read after the clear: %v", err)
	}

	// And the other direction: taking the OIDC provider away leaves the federation
	// standing, with its document and its attribute both intact.
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		if _, err := svc.SetSAML(ctx, tx, acme.ID, acmeFederation); err != nil {
			return err
		}
		_, err := svc.ClearOIDC(ctx, tx, acme.ID)
		return err
	}); err != nil {
		t.Fatalf("clear acme's OIDC provider: %v", err)
	}
	err = db.Run(tenancy.WithTenant(t.Context(), acme.Tenancy()), conn,
		func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			if _, ok, err := svc.OIDCOf(ctx, tx); err != nil {
				return err
			} else if ok {
				t.Error("acme answers an OIDC provider after ClearOIDC")
			}
			saml, ok, err := svc.SAMLSettingsOf(ctx, tx)
			if err != nil || !ok {
				t.Fatalf("acme lost its SAML provider with its OIDC one (ok=%v err=%v): ClearOIDC wrote columns that are not its own", ok, err)
			}
			if saml.EntityID != acmeFederation.EntityID {
				t.Errorf("acme's entity ID reads back %q after ClearOIDC, want %q", saml.EntityID, acmeFederation.EntityID)
			}
			return nil
		})
	if err != nil {
		t.Fatalf("read after the second clear: %v", err)
	}
}

// TestTheSAMLTrailCarriesTheEntityIDAndNotTheDocument is the auditable half stated as
// a set of JSON members. An IdP's metadata is public by construction, so this is not
// a secret — it is a few kilobytes of XML that answers no question a trail asks, in
// every copy the trail makes. The second assertion is the one an operator asks
// afterwards: which provider this one replaced.
func TestTheSAMLTrailCarriesTheEntityIDAndNotTheDocument(t *testing.T) {
	admin, conn := dbtest.Schema(t, user.Migrations, auth.Migrations)
	acme, _ := acmeSAML(t, conn)
	svc := internal.NewService(nil, tenanttest.InstallationLanguages(), "")

	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		switched := acmeFederation
		switched.EntityID = "urn:pkit:acme:new"
		_, err := svc.SetSAML(ctx, tx, acme.ID, switched)
		return err
	}); err != nil {
		t.Fatalf("switch acme's federation: %v", err)
	}

	rows, err := admin.QueryContext(t.Context(), `
		SELECT string_agg(k, ',' ORDER BY k), coalesce(max(o.payload->>'entityId'), ''),
		       coalesce(max(o.payload->>'wasEntityId'), ''), coalesce(max(o.payload->>'replaced'), '')
		FROM platformkit_outbox o, jsonb_object_keys(o.payload) AS k
		WHERE o.name = $1 AND o.tenant_id = $2
		GROUP BY o.id
		ORDER BY min(o.created_at)`, contracts.EventSAMLSet, acme.ID)
	if err != nil {
		t.Fatalf("read the trail: %v", err)
	}
	defer rows.Close()
	var seen [][4]string
	for rows.Next() {
		var one [4]string
		if err := rows.Scan(&one[0], &one[1], &one[2], &one[3]); err != nil {
			t.Fatalf("read the trail: %v", err)
		}
		seen = append(seen, one)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read the trail: %v", err)
	}
	if len(seen) != 2 {
		t.Fatalf("the trail holds %d tenant.saml_set rows for acme, want two: the federation and the switch", len(seen))
	}
	const members = "at,emailAttribute,entityId,metadataUrl,registration,tenantId"
	if seen[0][0] != members {
		t.Errorf("tenant.saml_set carries [%s], want exactly [%s]: a member outside this list is a member nobody counted, and one of the ones a document could hide in",
			seen[0][0], members)
	}
	if seen[1][1] != "urn:pkit:acme:new" || seen[1][2] != acmeFederation.EntityID || seen[1][3] != "true" {
		t.Errorf("the switch's trail says entityId=%q wasEntityId=%q replaced=%q, want %q/%q/true — which provider was replaced is not recoverable afterwards",
			seen[1][1], seen[1][2], seen[1][3], "urn:pkit:acme:new", acmeFederation.EntityID)
	}
}

// TestTheFakeRefusesWhatTheDatabaseRefuses is the pair the two files exist to make
// impossible in either direction: the same settings refused by the service that
// writes a row and by the double a consumer tests against, with the same word in the
// refusal, and the same silence on a write that changed nothing. A fake that accepted
// a certificate-less metadata document would let a consumer's test pass and the same
// configuration would then refuse every person who tried to sign in.
func TestTheFakeRefusesWhatTheDatabaseRefuses(t *testing.T) {
	fake, _, _ := tenanttest.Installed()
	ctx := t.Context()
	acme, err := fake.Create(ctx, db.Tx[db.System]{}, contracts.NewTenant{Slug: "acme", Name: "Acme", Host: "acme.example.com"})
	if err != nil {
		t.Fatalf("the fake gained no tenant: %v", err)
	}

	for _, bad := range []struct {
		name string
		in   contracts.SAMLSettings
		says string
	}{
		{"no metadata source at all", contracts.SAMLSettings{EntityID: "urn:pkit:acme",
			EmailAttribute: "email"}, "metadata source"},
		{"an entity ID that is a name and not a URI", contracts.SAMLSettings{EntityID: "tenant-a",
			MetadataXML: idpMetadata, EmailAttribute: "email"}, "entityId"},
		{"an entity ID with a query", contracts.SAMLSettings{EntityID: "https://pkit.example/saml?tenant=acme",
			MetadataXML: idpMetadata, EmailAttribute: "email"}, "entityId"},
		{"a metadata document that is not XML", contracts.SAMLSettings{EntityID: "urn:pkit:acme",
			MetadataXML: "not xml", EmailAttribute: "email"}, "metadataXml"},
		{"a service provider's document, which cannot vouch for anybody",
			contracts.SAMLSettings{EntityID: "urn:pkit:acme", EmailAttribute: "email",
				MetadataXML: strings.Replace(idpMetadata, "IDPSSODescriptor", "SPSSODescriptor", 2)},
			"identity provider"},
		{"a document with no signing certificate",
			contracts.SAMLSettings{EntityID: "urn:pkit:acme", EmailAttribute: "email",
				MetadataXML: withoutCertificate(idpMetadata)},
			"signing certificate"},
		{"a document with no Redirect or POST location",
			contracts.SAMLSettings{EntityID: "urn:pkit:acme", EmailAttribute: "email",
				MetadataXML: withoutSSOLocation(idpMetadata)},
			"single sign-on location"},
		{"a metadata URL that is not https and not local", contracts.SAMLSettings{EntityID: "urn:pkit:acme",
			MetadataURL: "http://idp.remote.example/metadata", EmailAttribute: "email"}, "https"},
		{"no attribute named", contracts.SAMLSettings{EntityID: "urn:pkit:acme", MetadataXML: idpMetadata},
			"emailAttribute"},
		{"provision with no roles", contracts.SAMLSettings{EntityID: "urn:pkit:acme", MetadataXML: idpMetadata,
			EmailAttribute: "email", Registration: contracts.RegistrationProvision}, "no roles"},
		{"a mode that is none of the three", contracts.SAMLSettings{EntityID: "urn:pkit:acme",
			MetadataXML: idpMetadata, EmailAttribute: "email", Registration: "everyone"}, "disabled, existing or provision"},
	} {
		_, err := fake.SetSAML(ctx, db.Tx[db.System]{}, acme.ID, bad.in)
		switch {
		case err == nil:
			t.Errorf("the fake stored %s", bad.name)
		case !errors.Is(err, crud.ErrInvalid):
			t.Errorf("the fake refused %s with %v, want crud.ErrInvalid", bad.name, err)
		case !strings.Contains(err.Error(), bad.says):
			t.Errorf("the fake refused %s with %q, which does not name %q", bad.name, err, bad.says)
		}
		if n := countPublished(fake.Published(), contracts.EventSAMLSet); n != 0 {
			t.Fatalf("%s published %d tenant.saml_set events: a refusal changes nothing, so the trail must say nothing", bad.name, n)
		}
	}

	// The three shapes the refusals above carve out are each acceptable, and the
	// urn carve-out is the one that would otherwise be a refused product: an entity
	// ID is a name, and `urn:` is a legitimate scheme for one.
	for _, good := range []contracts.SAMLSettings{
		{EntityID: "urn:pkit:acme", MetadataXML: idpMetadata, EmailAttribute: "email"},
		{EntityID: "https://pkit.example/saml", MetadataURL: "https://idp.acme.example/metadata", EmailAttribute: "email"},
		{EntityID: "urn:pkit:acme", MetadataURL: "http://127.0.0.1:4321/metadata", EmailAttribute: "email",
			Registration: contracts.RegistrationProvision, Roles: []string{"member"}},
	} {
		if _, err := fake.SetSAML(ctx, db.Tx[db.System]{}, acme.ID, good); err != nil {
			t.Errorf("the fake refused a provider it should store (entityId=%q): %v", good.EntityID, err)
		}
	}

	// Writing the same provider again is not a second change: the trail says once,
	// and clearing twice says once more and no more.
	same := contracts.SAMLSettings{EntityID: "urn:pkit:last", MetadataXML: idpMetadata, EmailAttribute: "email"}
	before := countPublished(fake.Published(), contracts.EventSAMLSet)
	for i := 0; i < 2; i++ {
		if _, err := fake.SetSAML(ctx, db.Tx[db.System]{}, acme.ID, same); err != nil {
			t.Fatalf("set the provider the second time: %v", err)
		}
	}
	if n := countPublished(fake.Published(), contracts.EventSAMLSet) - before; n != 1 {
		t.Errorf("two identical writes published %d tenant.saml_set events, want one: a retry must not read as two changes in an audit trail", n)
	}
	for i := 0; i < 2; i++ {
		if _, err := fake.ClearSAML(ctx, db.Tx[db.System]{}, acme.ID); err != nil {
			t.Fatalf("clear the provider the second time: %v", err)
		}
	}
	if n := countPublished(fake.Published(), contracts.EventSAMLCleared); n != 1 {
		t.Errorf("two clears published %d tenant.saml_cleared events, want one", n)
	}
	if _, err := fake.ClearSAML(ctx, db.Tx[db.System]{}, uuid.New()); !errors.Is(err, crud.ErrNotFound) {
		t.Errorf("clearing a tenant that does not exist answered %v, want crud.ErrNotFound", err)
	}
}

// countPublished is one event name counted in a fake's trail.
func countPublished(published []string, name string) int {
	n := 0
	for _, got := range published {
		if got == name {
			n++
		}
	}
	return n
}

// withoutCertificate and withoutSSOLocation are the two documents a tenant must not
// be able to save: each is a provider that could never complete a sign-in, and the
// moment to say so is the write, not the first person turned away at the door.
func withoutCertificate(document string) string {
	start := strings.Index(document, "<KeyDescriptor")
	end := strings.Index(document, "</KeyDescriptor>")
	return document[:start] + document[end+len("</KeyDescriptor>"):]
}

func withoutSSOLocation(document string) string {
	start := strings.Index(document, "<SingleSignOnService")
	end := strings.Index(document, "/>")
	return document[:start] + document[end+len("/>"):]
}
