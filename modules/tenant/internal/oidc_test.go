package internal_test

// T-0117's own cases for the per-tenant provider. The conformance suite covers
// what a command refuses and what it says; these cover the three claims that
// live below the command, in the schema and the policy, and that a suite run
// inside one cross-tenant transaction cannot reach:
//
//   - `OIDCOf` is answered from the transaction the `Host` header resolved, so
//     two tenants in one process read two different providers — or one and
//     none. That is the whole of "an issuer per tenant": the module asks its
//     composition, and the composition asks this row.
//   - A tenant transaction may read its own provider and may not choose it
//     (migrations/000030 says the columns ride the policy 000006 puts over
//     `tenants`, under which a tenant writes nothing on this table). Which of
//     the two directories a company's people live in is a decision made *about*
//     a customer, by whoever holds the control plane, and a tenant that could
//     repoint its own issuer could point its people's credentials anywhere.
//   - Half a provider cannot be stored, not merely refused by Go: the CHECK is
//     the last thing between a hand-written UPDATE and a tenant whose sign-in
//     page redirects to a provider that will refuse the code.
//
// And the trail: which members `tenant.oidc_set` actually carries. The claim
// under test is that no member of it can be a secret, which is checkable only
// as an exact set — a payload nobody enumerates is a payload somebody will add
// a field to.

import (
	"context"
	"strconv"
	"strings"
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

// acmePeople is what an operator would actually configure for a company whose
// people live in their own directory: a realm issuer, the installation's own
// client, and the *name* of the variable that holds the secret.
var acmePeople = contracts.OIDCSettings{
	Issuer: "https://idp.acme.example/realms/people", ClientID: "acme-portal",
	SecretRef: "PLATFORMKIT_OIDC_ACME_SECRET", RedirectPath: "/oidc/callback",
}

// twoTenants creates acme, with a provider, and globex, without one, and hands
// back the pair. Two tenants and not one, because every claim below is about
// one tenant not seeing the other's door.
func twoTenants(t *testing.T, conn *db.Conn) (*contracts.Tenant, *contracts.Tenant) {
	t.Helper()
	svc := internal.NewService(nil, tenanttest.InstallationLanguages())
	// The installation's own tenant before any customer's: `Create` mirrors its audit
	// row into that tenant's trail and refuses when there is none to write it into,
	// so a fixture with no operator tenant is one no lifecycle verb can run in. The
	// world gains a tenant here; no assertion below changes.
	installed(t, conn, svc)
	var acme, globex *contracts.Tenant
	err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		var err error
		if acme, err = svc.Create(ctx, tx, contracts.NewTenant{Slug: "acme", Name: "Acme", Host: "acme.example.com"}); err != nil {
			return err
		}
		if _, err = svc.SetOIDC(ctx, tx, acme.ID, acmePeople); err != nil {
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

// TestTwoTenantsReadTwoProvidersFromTheirOwnTransactions is the brief's claim
// at the layer the tenant module owns: the same service, asked in each tenant's
// own transaction, answers with that tenant's provider — and with nothing for
// the tenant nobody configured, which is the answer that makes its /oidc/start
// a 404 rather than a redirect to somebody else's door.
func TestTwoTenantsReadTwoProvidersFromTheirOwnTransactions(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, auth.Migrations)
	acme, globex := twoTenants(t, conn)
	svc := internal.NewService(nil, tenanttest.InstallationLanguages())

	for _, want := range []struct {
		who    *contracts.Tenant
		issuer string
	}{
		{acme, acmePeople.Issuer},
		{globex, ""},
	} {
		err := db.Run(tenancy.WithTenant(t.Context(), want.who.Tenancy()), conn,
			func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				got, ok, err := svc.OIDCOf(ctx, tx)
				if err != nil {
					return err
				}
				if want.issuer == "" {
					if ok {
						t.Errorf("%s has a provider (%v) and nobody configured one; its sign-in would dial somebody else's",
							want.who.Slug, got)
					}
					return nil
				}
				if !ok {
					t.Errorf("%s has no provider, want %s", want.who.Slug, want.issuer)
					return nil
				}
				if got.Issuer != want.issuer || got.ClientID != acmePeople.ClientID ||
					got.SecretRef != acmePeople.SecretRef {
					t.Errorf("read back issuer=%q clientId=%q secretRef=%q, want %q/%q/%q",
						got.Issuer, got.ClientID, got.SecretRef,
						acmePeople.Issuer, acmePeople.ClientID, acmePeople.SecretRef)
				}
				// The mode arrives with its default spelled out: the sign-in page
				// says what it will do with an address it has no account for, and
				// "" is not a thing it can say.
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

// TestATenantCannotRepointItsOwnIssuer is the control-plane half of the row:
// the columns ride the policy over `tenants`, and a tenant transaction that
// tries to move them does not. The assertion is the stored fact rather than a
// status code, because row-level security answers such a write in whichever of
// two ways it chooses — the WITH CHECK clause refuses it outright, the USING
// clause matches no row — and both mean the same thing to the tenant while a
// caller must not be able to mistake either for success.
func TestATenantCannotRepointItsOwnIssuer(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, auth.Migrations)
	acme, _ := twoTenants(t, conn)

	attempt := ""
	err := db.Run(tenancy.WithTenant(t.Context(), acme.Tenancy()), conn,
		func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			res := tx.DB().Table("tenants").Where("id = ?", acme.ID).
				Updates(map[string]any{"oidc_issuer": "https://idp.evil.example", "oidc_client_id": "evil"})
			switch {
			case res.Error != nil:
				attempt = "refused by the policy: " + res.Error.Error()
			case res.RowsAffected != 0:
				attempt = "wrote " + strconv.FormatInt(res.RowsAffected, 10) + " rows"
			default:
				attempt = "matched no row"
			}
			// nil, so the outer call reports what the write did rather than what
			// the rollback cost; Postgres has already poisoned the transaction, so
			// nothing can come of it either way.
			return nil
		})
	if attempt == "" {
		t.Fatalf("acme's transaction never reached the write: %v", err)
	}
	if strings.HasPrefix(attempt, "wrote ") {
		t.Errorf("a tenant's own transaction moved its provider: %s", attempt)
	}

	var issuer []string
	err = dbtest.System(t.Context(), conn, func(_ context.Context, tx db.Tx[db.System]) error {
		return tx.DB().Table("tenants").Where("id = ?", acme.ID).Pluck("oidc_issuer", &issuer).Error
	})
	if err != nil {
		t.Fatalf("read the stored issuer: %v", err)
	}
	if len(issuer) != 1 || issuer[0] != acmePeople.Issuer {
		t.Errorf("acme's stored issuer is %v after %s, want one row at %q",
			issuer, attempt, acmePeople.Issuer)
	}
}

// TestHalfAProviderCannotBeStored is the CHECK in migrations/000030, tried at
// the database rather than through the command that would not have written it.
// The command's own refusals are conformance cases; this is the one that holds
// against a write nobody went through a command for.
func TestHalfAProviderCannotBeStored(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, auth.Migrations)
	acme, _ := twoTenants(t, conn)

	for _, half := range []struct {
		name  string
		query string
	}{
		{"an issuer with no client", "UPDATE tenants SET oidc_client_id = NULL WHERE id = $1"},
		{"provision with no roles", "UPDATE tenants SET oidc_registration = 'provision' WHERE id = $1"},
	} {
		err := dbtest.System(t.Context(), conn, func(_ context.Context, tx db.Tx[db.System]) error {
			return tx.DB().Exec(half.query, acme.ID).Error
		})
		if err == nil {
			t.Errorf("%s was stored: the CHECK in migrations/000030 does not hold", half.name)
			continue
		}
		if !strings.Contains(err.Error(), "tenants_oidc_complete") {
			t.Errorf("%s was refused by %v, want the tenants_oidc_complete constraint", half.name, err)
		}
	}
}

// TestTheTrailCarriesTheProviderAndOnlyTheNameOfTheSecret is the auditable
// half stated as a set of JSON members. A payload is copied into audit_events
// by modules/audit and travels wherever the trail goes, so the claim "no secret
// in the trail" is only checkable as an enumeration: a member nobody counted is
// a member somebody will add. The second assertion is the one an operator
// actually asks after the fact — which provider this one replaced — because it
// is not recoverable afterwards.
func TestTheTrailCarriesTheProviderAndOnlyTheNameOfTheSecret(t *testing.T) {
	admin, conn := dbtest.Schema(t, user.Migrations, auth.Migrations)
	acme, _ := twoTenants(t, conn)
	svc := internal.NewService(nil, tenanttest.InstallationLanguages())

	err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		in := acmePeople
		in.Issuer = "https://login.acme.example"
		_, err := svc.SetOIDC(ctx, tx, acme.ID, in)
		return err
	})
	if err != nil {
		t.Fatalf("switch acme's provider: %v", err)
	}

	rows, err := admin.QueryContext(t.Context(), `
		SELECT string_agg(k, ',' ORDER BY k), coalesce(max(o.payload->>'issuer'), ''),
		       coalesce(max(o.payload->>'wasIssuer'), ''), coalesce(max(o.payload->>'replaced'), ''),
		       coalesce(max(o.payload->>'secretRef'), ''), coalesce(max(o.payload->>'registration'), '')
		FROM platformkit_outbox o, jsonb_object_keys(o.payload) AS k
		WHERE o.name = $1 AND o.tenant_id = $2
		GROUP BY o.id
		ORDER BY min(o.created_at)`, contracts.EventOIDCSet, acme.ID)
	if err != nil {
		t.Fatalf("read the trail: %v", err)
	}
	defer rows.Close()
	type trail struct {
		keys, issuer, was, replaced, ref, registration string
	}
	var seen []trail
	for rows.Next() {
		var one trail
		if err := rows.Scan(&one.keys, &one.issuer, &one.was, &one.replaced, &one.ref, &one.registration); err != nil {
			t.Fatalf("read the trail: %v", err)
		}
		seen = append(seen, one)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read the trail: %v", err)
	}
	if len(seen) != 2 {
		t.Fatalf("the trail holds %d tenant.oidc_set rows for acme, want two: the first provider and the switch", len(seen))
	}
	const firstMembers = "at,clientId,issuer,redirectPath,registration,secretRef,tenantId"
	if seen[0].keys != firstMembers {
		t.Errorf("tenant.oidc_set carries [%s], want exactly [%s]: a member outside this list is a member that could hold a secret",
			seen[0].keys, firstMembers)
	}
	if seen[0].ref != acmePeople.SecretRef {
		t.Errorf("the trail's secretRef is %q, want the reference %q", seen[0].ref, acmePeople.SecretRef)
	}
	if seen[0].was != "" || seen[0].replaced != "" {
		t.Errorf("the first provider was reported as a replacement: wasIssuer=%q replaced=%q, want both members absent", seen[0].was, seen[0].replaced)
	}
	if seen[1].issuer != "https://login.acme.example" || seen[1].was != acmePeople.Issuer || seen[1].replaced != "true" {
		t.Errorf("the switch says issuer=%q wasIssuer=%q replaced=%s; being switched from under a provider and being given one are different facts",
			seen[1].issuer, seen[1].was, seen[1].replaced)
	}
	// The mode is the default spelled out, on both rows: a trail that says ""
	// answers nothing about what the tenant will do with an unknown address.
	if seen[0].registration != contracts.RegistrationExisting {
		t.Errorf("the trail says registration %q, want %q", seen[0].registration, contracts.RegistrationExisting)
	}
	const secondMembers = "at,clientId,issuer,redirectPath,registration,replaced,secretRef,tenantId,wasClientId,wasIssuer"
	if seen[1].keys != secondMembers {
		t.Errorf("the switch's payload carries [%s], want exactly [%s]", seen[1].keys, secondMembers)
	}
}
